// Package cli provides the versioned, headless Sift command interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinghaihan/sift/internal/store"
	"github.com/jinghaihan/sift/internal/workflow"
)

func Handles(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "analyze", "draft", "validate", "import", "export", "script", "help":
		return true
	}
	return false
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) int {
	encode := func(value any) {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(value)
	}
	fail := func(err error, code string) int {
		if errors.Is(err, store.ErrConcurrentWrite) {
			code = "concurrent_write"
		}
		encode(map[string]any{"version": 1, "ok": false, "error": map[string]any{"code": code, "message": err.Error()}})
		return 1
	}
	if len(args) == 0 || args[0] == "help" {
		encode(map[string]any{"version": 1, "commands": []string{"analyze", "draft list", "draft upsert", "validate", "import", "export", "script show", "script upsert", "script validate", "script export"}, "flags": "--repo PATH --branch NAME --data-dir PATH [--integration REF --start COMMIT] [--bundle FILE] [--input FILE|-] [--dry-run]", "editHeader": map[string]any{"version": 1, "revision": "from analyze", "reviewVersion": "from analyze.review.version"}})
		return 0
	}
	command := args[0]
	args = args[1:]
	if command == "draft" || command == "script" {
		if len(args) == 0 {
			return fail(fmt.Errorf("%s requires an action", command), "usage")
		}
		command += " " + args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", ".", "Git repository")
	branch := flags.String("branch", "", "Feature branch")
	dataDir := flags.String("data-dir", "", "Review data directory (default ~/.sift or SIFT_DATA_DIR)")
	integration := flags.String("integration", "", "Integration branch")
	start := flags.String("start", "", "First feature commit")
	bundlePath := flags.String("bundle", "", "Portable bundle to reconstruct feature provenance")
	inputPath := flags.String("input", "-", "Structured input file or stdin")
	outputPath := flags.String("output", "-", "Export output file or stdout")
	format := flags.String("format", "json", "Export format: json or markdown")
	dry := flags.Bool("dry-run", false, "Validate without persisting")
	allowStale := flags.Bool("allow-stale", false, "Retain imported stale anchors explicitly")
	policy := flags.String("on-conflict", "error", "Import conflict policy: error, keep-local, incoming")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Run(ctx, []string{"help"}, stdin, stdout)
		}
		return fail(err, "usage")
	}
	if flags.NArg() != 0 {
		return fail(fmt.Errorf("unexpected positional arguments"), "usage")
	}
	read := func(path string) ([]byte, error) {
		var reader io.Reader = stdin
		var f *os.File
		var err error
		if path != "-" {
			f, err = os.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			reader = f
		}
		raw, err := io.ReadAll(io.LimitReader(reader, (32<<20)+1))
		if len(raw) > 32<<20 {
			return nil, fmt.Errorf("input exceeds 32 MB")
		}
		return raw, err
	}
	var bundle *store.ReviewFile
	if command == "import" || *bundlePath != "" {
		path := *bundlePath
		if command == "import" {
			path = *inputPath
		}
		raw, err := read(path)
		if err != nil {
			return fail(err, "input")
		}
		file, err := store.ReadReview(strings.NewReader(string(raw)))
		if err != nil {
			return fail(err, "input")
		}
		bundle = &file
		if *branch == "" {
			*branch = file.Scope.Branch
		}
	}
	session, err := workflow.Open(ctx, workflow.Request{Repository: *repo, Branch: *branch, Integration: *integration, Start: *start, DataDir: *dataDir, Bundle: bundle})
	if err != nil {
		return fail(err, "analysis")
	}
	defer session.Close()
	result := func(record store.Record) {
		encode(map[string]any{"version": 1, "ok": true, "dryRun": *dry, "revision": session.Result.Tip, "reviewVersion": record.Sequence, "review": record})
	}
	decode := func(target any) error {
		raw, err := read(*inputPath)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(target); err != nil {
			return err
		}
		if err = decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("expected one JSON document")
		}
		return nil
	}
	writeExport := func(raw []byte) int {
		if *dry {
			encode(map[string]any{"version": 1, "ok": true, "dryRun": true, "bytes": len(raw)})
			return 0
		}
		if *outputPath == "-" {
			if _, err := stdout.Write(raw); err != nil {
				return fail(err, "output")
			}
			return 0
		}
		output, err := filepath.Abs(*outputPath)
		if err != nil {
			return fail(err, "output")
		}
		if rel, e := filepath.Rel(session.Result.Repository, output); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("export destination must be outside the reviewed repository"), "output")
		}
		if err = os.WriteFile(output, raw, 0o600); err != nil {
			return fail(err, "output")
		}
		encode(map[string]any{"version": 1, "ok": true, "output": output})
		return 0
	}
	switch command {
	case "analyze":
		encode(map[string]any{"version": 1, "ok": true, "scope": session.Result.Scope, "revision": session.Result.Tip, "diff": session.Result.Patch, "historical": session.Result.Uncertain, "provenance": session.Result.Provenance, "review": map[string]any{"version": session.Record.Sequence, "revision": session.Record.Tip, "comments": session.Record.Comments, "script": session.Record.Script}})
	case "draft list":
		result(session.Record)
	case "draft upsert":
		var batch workflow.DraftBatch
		if err = decode(&batch); err != nil {
			return fail(err, "input")
		}
		record, err := session.UpsertDrafts(ctx, batch, *dry)
		if err != nil {
			return fail(err, "validation")
		}
		result(record)
	case "script show":
		encode(map[string]any{"version": 1, "ok": true, "revision": session.Result.Tip, "reviewVersion": session.Record.Sequence, "sections": session.Record.Script})
	case "script upsert":
		var batch workflow.ScriptBatch
		if err = decode(&batch); err != nil {
			return fail(err, "input")
		}
		record, err := session.UpsertScript(ctx, batch, *dry)
		if err != nil {
			return fail(err, "validation")
		}
		result(record)
	case "validate", "script validate":
		validation := session.Validate(ctx)
		if command == "script validate" {
			validation.Comments = nil
			validation.Valid = true
			for _, ref := range validation.References {
				if ref.Status != "valid" {
					validation.Valid = false
				}
			}
		}
		encode(map[string]any{"version": 1, "ok": validation.Valid, "validation": validation})
		if !validation.Valid {
			return 2
		}
	case "import":
		if *policy != "error" && *policy != "keep-local" && *policy != "incoming" {
			return fail(fmt.Errorf("invalid conflict policy"), "usage")
		}
		record, err := session.Import(ctx, *bundle, *dry, *allowStale, *policy)
		if err != nil {
			return fail(err, "validation")
		}
		result(record)
	case "export", "script export":
		exported := session.Record.Clone()
		exported.Provenance = session.Result.Provenance
		if exported.Tip == "" {
			exported.Tip = session.Result.Tip
		}
		if command == "script export" && *format == "markdown" {
			return writeExport([]byte(store.ScriptMarkdown(exported)))
		}
		if *format == "markdown" {
			return writeExport([]byte(store.Markdown(exported)))
		}
		if *format != "json" {
			return fail(fmt.Errorf("unknown export format"), "usage")
		}
		file := store.ExportReview(exported, filepath.Base(session.Result.Repository), "")
		if err = file.Validate(); err != nil {
			return fail(err, "validation")
		}
		raw, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return fail(err, "output")
		}
		return writeExport(append(raw, '\n'))
	default:
		return fail(fmt.Errorf("unknown command %q", command), "usage")
	}
	return 0
}

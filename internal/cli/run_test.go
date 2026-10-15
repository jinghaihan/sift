package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jinghaihan/sift/internal/review"
	"github.com/jinghaihan/sift/internal/store"
	"github.com/jinghaihan/sift/internal/workflow"
)

type response struct {
	OK            bool                           `json:"ok"`
	Revision      string                         `json:"revision"`
	ReviewVersion uint64                         `json:"reviewVersion"`
	Review        store.Record                   `json:"review"`
	Error         struct{ Code, Message string } `json:"error"`
	Validation    workflow.Validation            `json:"validation"`
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Sift Test", "GIT_AUTHOR_EMAIL=sift@example.test", "GIT_COMMITTER_NAME=Sift Test", "GIT_COMMITTER_EMAIL=sift@example.test")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, raw)
	}
	return strings.TrimSpace(string(raw))
}

func codeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "dev")
	git(t, repo, "config", "commit.gpgSign", "false")
	git(t, repo, "config", "core.autocrlf", "false")
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package main\n\nfunc run() {\n\toldCall()\n}\n")
	write("b.go", "oldSecond()\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "base")
	git(t, repo, "checkout", "-b", "feature")
	write("a.go", "package main\n\nfunc run() {\n\tnewCall()\n}\n")
	write("b.go", "newSecond()\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "feature")
	return repo
}

func invoke(t *testing.T, repo, dir string, command []string, input any) (response, int, []byte) {
	t.Helper()
	var raw []byte
	var err error
	if data, ok := input.([]byte); ok {
		raw = data
	} else if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	args := append(slices.Clone(command), "--repo", repo, "--branch", "feature", "--data-dir", dir)
	code := Run(context.Background(), args, bytes.NewReader(raw), &out)
	var result response
	if err = json.Unmarshal(out.Bytes(), &result); err != nil && !slices.Contains(args, "markdown") {
		t.Fatalf("not one JSON response: %v %s", err, &out)
	}
	return result, code, out.Bytes()
}

func TestHeadlessDraftAndScriptRoundTrip(t *testing.T) {
	repo, dir := codeRepo(t), filepath.Join(t.TempDir(), "new-data")
	before := git(t, repo, "status", "--porcelain=v1") + git(t, repo, "rev-parse", "HEAD")
	result, code, _ := invoke(t, repo, dir, []string{"analyze"}, nil)
	if code != 0 || !result.OK || len(result.Revision) != 40 {
		t.Fatalf("analyze: %+v", result)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read-only analyze created a data directory")
	}
	header := workflow.EditHeader{Version: 1, Revision: result.Revision, ReviewVersion: new(uint64)}
	batch := workflow.DraftBatch{EditHeader: header, Owner: "bot", Comments: []workflow.Draft{{ID: "one", Path: "a.go", Side: "new", Start: 4, Body: "Explain this change."}}}
	dry, code, _ := invoke(t, repo, dir, []string{"draft", "upsert", "--dry-run"}, batch)
	if code != 0 || len(dry.Review.Comments) != 1 {
		t.Fatalf("dry upsert: %+v", dry)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("dry run wrote data")
	}
	written, code, _ := invoke(t, repo, dir, []string{"draft", "upsert"}, batch)
	if code != 0 || len(written.Review.Comments) != 1 {
		t.Fatalf("draft upsert: %+v", written)
	}
	id, created := written.Review.Comments[0].ID, written.Review.Comments[0].Created
	db, human, err := store.Open(dir, written.Review.Scope)
	if err != nil {
		t.Fatal(err)
	}
	human.Comments = append(human.Comments, store.NewComment("b.go", "b.go", "Human note.", store.Anchor{Side: 1, Start: 1, End: 1, Excerpt: "newSecond()"}))
	human.Sequence++
	if err = db.Save(human); err != nil {
		t.Fatal(err)
	}
	stale, code, _ := invoke(t, repo, dir, []string{"draft", "upsert"}, batch)
	if code == 0 || stale.Error.Code != "concurrent_write" {
		t.Fatalf("stale update accepted: %+v", stale)
	}
	current, _, _ := invoke(t, repo, dir, []string{"draft", "list"}, nil)
	batch.ReviewVersion = &current.ReviewVersion
	batch.Comments[0].ID, batch.Comments[0].Body = id, "Updated agent note."
	written, code, _ = invoke(t, repo, dir, []string{"draft", "upsert"}, batch)
	if code != 0 || len(written.Review.Comments) != 2 || written.Review.Comments[1].Body != "Human note." || !written.Review.Comments[0].Created.Equal(created) {
		t.Fatalf("upsert lost or duplicated drafts: %+v", written)
	}
	title, body := "核心变化", "Explain **why** this change is needed."
	script := workflow.ScriptBatch{EditHeader: workflow.EditHeader{Version: 1, Revision: written.Revision, ReviewVersion: &written.ReviewVersion}, Sections: []workflow.SectionEdit{
		{ID: "first", Title: &title, Body: &body, References: []workflow.ReferenceEdit{{ID: "comment", Kind: "comment", CommentID: id}}},
		{ID: "second", Title: ptr("Context"), References: []workflow.ReferenceEdit{{ID: "context", Kind: "code", Path: "a.go", Side: "new", Start: 2}}},
		{ID: "third", Title: ptr("Second file"), References: []workflow.ReferenceEdit{{ID: "second-file", Kind: "code", Path: "b.go", Side: "new", Start: 1}}},
	}, Order: []string{"third", "first", "second"}}
	written, code, _ = invoke(t, repo, dir, []string{"script", "upsert"}, script)
	if code != 0 || len(written.Review.Script) != 3 || written.Review.Script[0].ID != "third" {
		t.Fatalf("script: %+v", written)
	}
	valid, code, _ := invoke(t, repo, dir, []string{"script", "validate"}, nil)
	if code != 0 || !valid.Validation.Valid || len(valid.Validation.References) != 3 {
		t.Fatalf("validate: %+v", valid)
	}
	_, code, markdown := invoke(t, repo, dir, []string{"script", "export", "--format", "markdown"}, nil)
	if code != 0 || strings.Index(string(markdown), "## Second file") > strings.Index(string(markdown), "## 核心变化") || !bytes.Contains(markdown, []byte("a.go:4")) {
		t.Fatalf("ordered readable Markdown: %s", markdown)
	}
	_, code, bundle := invoke(t, repo, dir, []string{"export"}, nil)
	if code != 0 {
		t.Fatalf("export: %s", bundle)
	}
	portable, err := store.ReadReview(bytes.NewReader(bundle))
	if err != nil || portable.Version != 2 || len(portable.Provenance) == 0 || len(portable.Script) != 3 {
		t.Fatalf("portable review: %+v %v", portable, err)
	}
	other := filepath.Join(t.TempDir(), "import-data")
	_, code, out := invoke(t, repo, other, []string{"import", "--dry-run"}, bundle)
	if code != 0 {
		t.Fatalf("dry import: %s", out)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("dry import wrote data")
	}
	imported, code, _ := invoke(t, repo, other, []string{"import"}, bundle)
	if code != 0 || len(imported.Review.Script) != 3 || len(imported.Review.Comments) != 2 {
		t.Fatalf("import: %+v", imported)
	}
	_, code, _ = invoke(t, repo, other, []string{"import"}, bundle)
	if code != 0 {
		t.Fatal("repeated import was not idempotent")
	}
	// An explicitly targeted section update keeps other manual content/order.
	script.EditHeader.ReviewVersion = &written.ReviewVersion
	script.Sections = []workflow.SectionEdit{{ID: "first", Body: ptr("Revised explanation.")}}
	script.Order = nil
	updated, code, _ := invoke(t, repo, dir, []string{"script", "upsert"}, script)
	if code != 0 || len(updated.Review.Script) != 3 || updated.Review.Script[0].ID != "third" || len(updated.Review.Script[1].References) != 1 {
		t.Fatalf("partial update lost untargeted content: %+v", updated)
	}
	// Deleting a linked comment must be visible in validation.
	db, record, err := store.Open(dir, updated.Review.Scope)
	if err != nil {
		t.Fatal(err)
	}
	record.Comments = slices.DeleteFunc(record.Comments, func(c store.Comment) bool { return c.ID == id })
	record.Sequence++
	if err = db.Save(record); err != nil {
		t.Fatal(err)
	}
	missing, code, _ := invoke(t, repo, dir, []string{"script", "validate"}, nil)
	if code != 2 || !slices.ContainsFunc(missing.Validation.References, func(l workflow.Location) bool { return l.Status == "missing_comment" }) {
		t.Fatalf("deleted reference was hidden: %+v", missing)
	}
	after := git(t, repo, "status", "--porcelain=v1") + git(t, repo, "rev-parse", "HEAD")
	if before != after {
		t.Fatal("headless workflow modified the reviewed repository")
	}
}

func ptr(value string) *string { return &value }

func TestScriptReferencesBecomeStaleAndCanBeRepaired(t *testing.T) {
	repo, dir := codeRepo(t), t.TempDir()
	current, _, _ := invoke(t, repo, dir, []string{"analyze"}, nil)
	version := uint64(0)
	script := workflow.ScriptBatch{EditHeader: workflow.EditHeader{Version: 1, Revision: current.Revision, ReviewVersion: &version}, Sections: []workflow.SectionEdit{{ID: "section", Title: ptr("Explain"), References: []workflow.ReferenceEdit{
		{ID: "context", Kind: "code", Path: "a.go", Side: "new", Start: 3},
		{ID: "changed", Kind: "code", Path: "b.go", Side: "new", Start: 1},
	}}}}
	written, code, _ := invoke(t, repo, dir, []string{"script", "upsert"}, script)
	if code != 0 {
		t.Fatalf("prepare references: %+v", written)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("// Added later.\npackage main\n\nfunc run() {\n\tnewCall()\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("replacement()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "later feature work")
	validation, code, _ := invoke(t, repo, dir, []string{"script", "validate"}, nil)
	if code != 2 || validation.Validation.References[0].Status != "stale" || validation.Validation.References[0].Anchor.Start != 4 || validation.Validation.References[1].Status != "unresolved" {
		t.Fatalf("old references were silently treated as current: %+v", validation)
	}
	script.ReviewVersion = &written.ReviewVersion
	stale, code, _ := invoke(t, repo, dir, []string{"script", "upsert"}, script)
	if code == 0 || !strings.Contains(stale.Error.Message, "stale revision") {
		t.Fatalf("accepted old revision: %+v", stale)
	}
	now, _, _ := invoke(t, repo, dir, []string{"script", "show"}, nil)
	script.EditHeader = workflow.EditHeader{Version: 1, Revision: now.Revision, ReviewVersion: &now.ReviewVersion}
	// With no new line, uniquely relocate the existing context. The changed
	// reference requires an explicit replacement location.
	script.Sections[0].References = []workflow.ReferenceEdit{{ID: "context"}, {ID: "changed", Start: 1}}
	repaired, code, _ := invoke(t, repo, dir, []string{"script", "upsert"}, script)
	if code != 0 || repaired.Review.Script[0].References[0].Anchor.Start != 4 {
		t.Fatalf("repair: %+v", repaired)
	}
	valid, code, _ := invoke(t, repo, dir, []string{"script", "validate"}, nil)
	if code != 0 || !valid.Validation.Valid {
		t.Fatalf("repaired refs remain stale: %+v", valid)
	}
}

func TestBundleRestoresProvenanceBeforeAnalysis(t *testing.T) {
	repo := codeRepo(t)
	start := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "dev")
	git(t, repo, "merge", "--ff-only", "feature")
	if err := os.WriteFile(filepath.Join(repo, "upstream.txt"), []byte("unrelated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "upstream")
	git(t, repo, "checkout", "feature")
	git(t, repo, "merge", "--ff-only", "dev")
	result, err := review.Analyze(t.Context(), review.Options{Scope: review.Scope{Repository: repo, Branch: "feature", Integration: "dev", Start: start}})
	if err != nil {
		t.Fatal(err)
	}
	bundle := store.ExportReview(store.Record{Scope: result.Scope, Tip: result.Tip, Provenance: result.Provenance}, "test", "")
	raw, _ := json.Marshal(bundle)
	git(t, repo, "reflog", "expire", "--expire=all", "--all")
	dir := filepath.Join(t.TempDir(), "data")
	imported, code, out := invoke(t, repo, dir, []string{"import"}, raw)
	if code != 0 || len(imported.Review.Provenance) == 0 {
		t.Fatalf("provenance was not restored before analysis: %s", out)
	}
	_, code, out = invoke(t, repo, dir, []string{"analyze"}, nil)
	if code != 0 || bytes.Contains(out, []byte("diff --git a/upstream.txt")) {
		t.Fatalf("reopened review lost ownership: %s", out)
	}
}

// Package workflow shares headless review operations with the native app.
package workflow

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jinghaihan/sift/internal/diff"
	"github.com/jinghaihan/sift/internal/review"
	"github.com/jinghaihan/sift/internal/store"
)

type Request struct {
	Repository, Branch, Integration, Start, DataDir string
	Bundle                                          *store.ReviewFile
}

type Session struct {
	Result   review.Result
	Record   store.Record
	Database *store.Database
	DataDir  string
	cache    map[string]map[int]string
}

func Open(ctx context.Context, request Request) (*Session, error) {
	info, err := review.Inspect(ctx, request.Repository)
	if err != nil {
		return nil, err
	}
	if request.Branch == "" {
		request.Branch = info.Branch
	}
	if request.Branch == "" {
		return nil, fmt.Errorf("select a branch for this review")
	}
	if request.DataDir == "" {
		request.DataDir, err = store.DefaultDirectory()
		if err != nil {
			return nil, err
		}
	}
	request.DataDir, err = filepath.Abs(request.DataDir)
	if err != nil {
		return nil, err
	}
	if rel, e := filepath.Rel(info.Path, request.DataDir); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("data directory must be outside the reviewed repository")
	}
	scope := review.Scope{Repository: info.Path, Branch: request.Branch, Integration: request.Integration, Start: request.Start}
	if request.Bundle != nil {
		if err = request.Bundle.Validate(); err != nil {
			return nil, err
		}
		if request.Bundle.Scope.Branch != request.Branch {
			return nil, fmt.Errorf("bundle belongs to branch %s", request.Bundle.Scope.Branch)
		}
		if request.Bundle.Scope.Historical != "" {
			return nil, fmt.Errorf("historical review bundles must be imported in the native historical view")
		}
		scope.Integration, scope.Start = request.Bundle.Scope.Integration, request.Bundle.Scope.Start
	} else if scope.Start == "" && scope.Integration == "" {
		scope, err = store.FindScope(request.DataDir, info.Path, request.Branch)
		if err != nil {
			return nil, err
		}
		if scope.Start == "" {
			scope, err = review.DetectScope(ctx, info.Path, request.Branch)
			if err != nil {
				return nil, err
			}
		}
	} else if scope.Start == "" || scope.Integration == "" {
		return nil, fmt.Errorf("integration and start must be provided together")
	}
	db, record, err := store.Open(request.DataDir, scope)
	if err != nil {
		return nil, err
	}
	known := record.Provenance
	if request.Bundle != nil {
		known = append(append([]review.CommitProvenance{}, known...), request.Bundle.Provenance...)
	}
	result, err := review.Analyze(ctx, review.Options{Scope: scope, KnownCommits: known, KeepSnapshot: true})
	if err != nil {
		return nil, err
	}
	if result.Scope != record.Scope {
		db, record, err = store.Open(request.DataDir, result.Scope)
		if err != nil {
			result.Close()
			return nil, err
		}
	}
	return &Session{Result: result, Record: record, Database: db, DataDir: request.DataDir}, nil
}

func (s *Session) Close() { _ = s.Result.Close() }

func (s *Session) Lines(ctx context.Context, path string, side int, historical string) (map[int]string, error) {
	if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00") || (side != 0 && side != 1) {
		return nil, fmt.Errorf("invalid code location")
	}
	key := fmt.Sprintf("%d\x00%s\x00%s", side, historical, path)
	if lines, ok := s.cache[key]; ok {
		return lines, nil
	}
	patch := s.Result.Patch
	ref := s.Result.Tip
	if side == 0 {
		ref = s.Result.Baseline
	}
	if historical != "" {
		found := false
		for _, item := range s.Result.Uncertain {
			if item.Commit == historical {
				patch = item.Patch
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("historical patch is unavailable")
		}
		ref = historical
		if side == 0 {
			ref += "^1"
		}
	}
	allowed := false
	for _, f := range diff.Parse([]byte(patch)) {
		if f.Path == path || side == 0 && f.OldPath == path {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("file is not in this review diff")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", s.Result.SnapshotPath, "show", ref+":"+path)
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cannot read code at this location")
	}
	if len(raw) > 32<<20 || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, fmt.Errorf("code file is too large or binary")
	}
	text := strings.TrimSuffix(string(raw), "\n")
	lines := map[int]string{}
	if len(raw) == 0 {
		return lines, nil
	}
	for i, line := range strings.Split(text, "\n") {
		lines[i+1] = line
	}
	if s.cache == nil {
		s.cache = map[string]map[int]string{}
	}
	s.cache[key] = lines
	return lines, nil
}

func (s *Session) Anchor(ctx context.Context, path string, side, start, end int, historical string) (store.Anchor, error) {
	lines, err := s.Lines(ctx, path, side, historical)
	if err != nil {
		return store.Anchor{}, err
	}
	if start < 1 || end < start || end-start >= 10000 {
		return store.Anchor{}, fmt.Errorf("invalid line range")
	}
	var excerpt []string
	for i := start; i <= end; i++ {
		line, ok := lines[i]
		if !ok {
			return store.Anchor{}, fmt.Errorf("line %d is outside the file", i)
		}
		excerpt = append(excerpt, line)
	}
	return store.Anchor{Side: side, Start: start, End: end, Excerpt: strings.Join(excerpt, "\n"), Before: lines[start-1], After: lines[end+1]}, nil
}

type Location struct {
	ID         string       `json:"id"`
	Section    string       `json:"section,omitempty"`
	Status     string       `json:"status"`
	Path       string       `json:"path,omitempty"`
	Historical string       `json:"historical,omitempty"`
	Anchor     store.Anchor `json:"anchor"`
	Message    string       `json:"message,omitempty"`
}

type Validation struct {
	Valid         bool       `json:"valid"`
	Revision      string     `json:"revision"`
	ReviewVersion uint64     `json:"reviewVersion"`
	Comments      []Location `json:"comments"`
	References    []Location `json:"references"`
}

func (s *Session) locate(ctx context.Context, ref store.ScriptReference, comments []store.Comment) Location {
	location := Location{ID: ref.ID, Status: "valid", Path: ref.Path, Historical: ref.Historical, Anchor: ref.Anchor}
	if ref.Kind == "comment" {
		found := false
		for _, cm := range comments {
			if cm.ID == ref.CommentID {
				location.Path, location.Anchor = cm.Path, cm.Anchor
				found = true
				break
			}
		}
		if !found {
			location.Status, location.Message = "missing_comment", "Linked comment was deleted or is unavailable"
			return location
		}
	}
	lines, err := s.Lines(ctx, location.Path, location.Anchor.Side, ref.Historical)
	if err != nil {
		location.Status, location.Message = "unresolved", err.Error()
		return location
	}
	anchor, ok := store.Relocate(location.Anchor, lines)
	if !ok {
		location.Status, location.Message = "unresolved", "Code context cannot be located uniquely"
		return location
	}
	location.Anchor = anchor
	expected := s.Result.Tip
	if ref.Historical != "" {
		expected = ref.Historical
	}
	if ref.Revision != expected {
		location.Status = "stale"
	}
	return location
}

func (s *Session) Validate(ctx context.Context) Validation {
	v := Validation{Valid: true, Revision: s.Result.Tip, ReviewVersion: s.Record.Sequence}
	for _, cm := range s.Record.Comments {
		ref := store.ScriptReference{ID: cm.ID, Kind: "code", Path: cm.Path, Anchor: cm.Anchor, Revision: s.Record.Tip}
		location := s.locate(ctx, ref, nil)
		v.Comments = append(v.Comments, location)
		if location.Status != "valid" {
			v.Valid = false
		}
	}
	for _, section := range s.Record.Script {
		for _, ref := range section.References {
			comments := s.Record.Comments
			if ref.Historical != "" && ref.Kind == "comment" {
				scope := s.Result.Scope
				scope.Branch += "::historical:" + ref.Historical
				_, record, err := store.Open(s.DataDir, scope)
				if err == nil {
					comments = record.Comments
				} else {
					comments = nil
				}
			}
			location := s.locate(ctx, ref, comments)
			location.Section = section.ID
			v.References = append(v.References, location)
			if location.Status != "valid" {
				v.Valid = false
			}
		}
	}
	return v
}

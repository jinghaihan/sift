package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinghaihan/sift/internal/review"
)

func TestPersistenceAndLateSaves(t *testing.T) {
	dir := t.TempDir()
	scope := review.Scope{Repository: "/repo", Branch: "feature", Integration: "dev", Start: "start"}
	db, record, err := Open(dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	record.Comments = append(record.Comments, NewComment("feature:x.go", "x.go", "Please check this.", Anchor{Side: 1, Start: 4, End: 4, Excerpt: "call()"}))
	record.Provenance = []review.CommitProvenance{{ID: "commit", Role: "feature"}}
	old := record.Clone()
	old.Provenance[0].ID = "old"
	record.Sequence = 2
	record.Comments[0].Resolved = true
	record.Viewed["feature:x.go"] = "fingerprint"
	if err = db.Save(record); err != nil {
		t.Fatal(err)
	}
	if err = db.Save(old); err != nil {
		t.Fatal(err)
	}
	_, loaded, err := Open(dir, scope)
	if err != nil || len(loaded.Comments) != 1 || !loaded.Comments[0].Resolved || loaded.Viewed["feature:x.go"] != "fingerprint" {
		t.Fatalf("lost the latest review: %+v, %v", loaded, err)
	}
	if len(loaded.Provenance) != 1 || loaded.Provenance[0].ID != "commit" {
		t.Fatal("the saved feature provenance was lost or shared with a stale snapshot")
	}
	other := scope
	other.Start = "another start"
	_, isolated, err := Open(dir, other)
	if err != nil || len(isolated.Comments) != 0 {
		t.Fatal("review scopes share comments")
	}
}

func TestCorruptRecordIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	scope := review.Scope{Repository: "repo"}
	db, _, _ := Open(dir, scope)
	os.MkdirAll(filepath.Dir(db.path), 0o700)
	os.WriteFile(db.path, []byte("broken"), 0o600)
	if _, _, err := Open(dir, scope); err == nil {
		t.Fatal("accepted corrupt saved review")
	}
}

func TestAnchorsMoveAndBecomeOutdated(t *testing.T) {
	a := Anchor{Side: 1, Start: 2, End: 3, Excerpt: "one\ntwo", Before: "before", After: "after"}
	lines := map[int]string{10: "before", 11: "one", 12: "two", 13: "after"}
	moved, ok := Relocate(a, lines)
	if !ok || moved.Start != 11 || moved.End != 12 {
		t.Fatalf("anchor did not move: %+v, %v", moved, ok)
	}
	lines[12] = "changed"
	if _, ok = Relocate(a, lines); ok {
		t.Fatal("changed code kept its old anchor")
	}
	lines = map[int]string{10: "before", 11: "one", 12: "two", 13: "after", 20: "before", 21: "one", 22: "two", 23: "after"}
	if _, ok = Relocate(a, lines); ok {
		t.Fatal("ambiguous repeated code received a guessed anchor")
	}
}

func TestMarkdownIncludesContextAndStatus(t *testing.T) {
	r := Record{Scope: review.Scope{Branch: "feature", Integration: "dev", Start: "abc"}, Tip: "def", Comments: []Comment{
		{Path: "x.go", Body: "Check cancellation.", Resolved: true, Outdated: true, Anchor: Anchor{Side: 1, Start: 4, End: 5, Excerpt: "```\ncall()"}},
	}}
	text := Markdown(r)
	for _, want := range []string{"feature", "dev", "x.go:4–5", "Resolved · Outdated", "````", "Check cancellation."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in export:\n%s", want, text)
		}
	}
}

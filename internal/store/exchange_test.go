package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinghaihan/sift/internal/review"
)

func portableReview() ReviewFile {
	file := ExportReview(Record{Scope: review.Scope{Repository: "/private/local/checkout", Branch: "feature", Integration: "dev", Start: strings.Repeat("a", 40)}, Tip: strings.Repeat("b", 40), Comments: []Comment{
		NewComment("x.go", "x.go", "Check cancellation.", Anchor{Side: 1, Start: 4, End: 5, Excerpt: "first()\nsecond()", Before: "before", After: "after"}),
	}}, "example", "https://user:secret@example.com/owner/repo.git?token=secret#secret")
	file.Comments[0].Created = file.Comments[0].Created.UTC()
	return file
}

func TestReviewFileRoundTrip(t *testing.T) {
	want := portableReview()
	path := filepath.Join(t.TempDir(), "review.json")
	if err := WriteReview(path, want); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "/private/local") {
		t.Fatal("portable review leaked credentials or a local checkout path")
	}
	got, err := ReadReview(bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	for _, remote := range []string{"/Users/example/repo", "../repo", "file:///private/repo"} {
		if RepositoryRemote(remote) != "" {
			t.Fatal("exported a local origin path")
		}
	}
	if got := RepositoryRemote("git@example.com:owner/repo.git"); got != "ssh://example.com/owner/repo" {
		t.Fatalf("SSH remote: %s", got)
	}
}

func TestReviewFileRejectsInvalidImports(t *testing.T) {
	for name, change := range map[string]func(*ReviewFile){
		"version":       func(f *ReviewFile) { f.Version++ },
		"revision":      func(f *ReviewFile) { f.Revision = "HEAD" },
		"duplicate ID":  func(f *ReviewFile) { f.Comments = append(f.Comments, f.Comments[0]) },
		"absolute path": func(f *ReviewFile) { f.Comments[0].Path = "/etc/file" },
		"parent path":   func(f *ReviewFile) { f.Comments[0].Path = "../file" },
		"side":          func(f *ReviewFile) { f.Comments[0].Anchor.Side = 3 },
		"range":         func(f *ReviewFile) { f.Comments[0].Anchor.End = 10000000 },
		"empty body":    func(f *ReviewFile) { f.Comments[0].Body = " " },
	} {
		t.Run(name, func(t *testing.T) {
			file := portableReview()
			change(&file)
			data, _ := json.Marshal(file)
			if _, err := ReadReview(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid review accepted")
			}
		})
	}
	if _, err := ReadReview(strings.NewReader(`{} {}`)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}

func TestImportCommentsPreservesLocalEditsAndDeduplicates(t *testing.T) {
	original := portableReview().Comments[0]
	changed := original
	changed.Body = "Check cancellation and cleanup."
	newComment := NewComment("y.go", "y.go", "Handle the error.", Anchor{Side: 0, Start: 2, End: 2, Excerpt: "call()"})
	local, incoming := []Comment{original}, []Comment{changed, newComment}
	conflicts := CommentConflicts(local, incoming)
	if len(conflicts) != 1 || conflicts[0].Local.Body != original.Body || conflicts[0].Imported.Body != changed.Body {
		t.Fatalf("missing conflict: %+v", conflicts)
	}
	kept := MergeComments(local, incoming, nil)
	if len(kept) != 2 || kept[0].Body != original.Body || len(MergeComments(kept, incoming, nil)) != 2 {
		t.Fatal("import lost local text or duplicated a comment")
	}
	replaced := MergeComments(local, incoming, map[string]bool{original.ID: true})
	if replaced[0].Body != changed.Body || !replaced[0].Created.Equal(original.Created) {
		t.Fatal("explicit replacement lost imported data")
	}
	moved := original
	moved.Anchor.Start, moved.Anchor.End, moved.Outdated = 20, 21, true
	if len(CommentConflicts(local, []Comment{moved})) != 0 {
		t.Fatal("relocated lines were treated as a text conflict")
	}
}

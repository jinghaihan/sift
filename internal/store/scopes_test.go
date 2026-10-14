package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jinghaihan/sift/internal/review"
)

func TestScopesRemainIndependentAcrossProjectsAndBranches(t *testing.T) {
	dir := t.TempDir()
	scopes := []review.Scope{
		{Repository: "/project-a", Branch: "feature", Integration: "dev", Start: "a"},
		{Repository: "/project-b", Branch: "feature", Integration: "main", Start: "b"},
		{Repository: "/project-a", Branch: "another", Integration: "dev", Start: "c"},
	}
	for _, scope := range scopes {
		if err := SaveScope(dir, scope); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range scopes {
		got, err := LoadScope(dir, scope.Repository, scope.Branch)
		if err != nil || got != scope {
			t.Fatalf("range changed after switching projects: %+v, %v", got, err)
		}
	}
	for repository, want := range map[string]string{"/project-a": "another", "/project-b": "feature"} {
		got, err := LastBranch(dir, repository)
		if err != nil || got != want {
			t.Fatalf("last branch leaked between projects: %s, %v", got, err)
		}
	}
	if got, err := LoadScope(dir, "/project-c", "feature"); err != nil || got != (review.Scope{}) {
		t.Fatalf("new project inherited a scope: %+v, %v", got, err)
	}
}

func TestLegacyScopesMigrateWithoutChangingReviewRecords(t *testing.T) {
	dir := t.TempDir()
	a := review.Scope{Repository: "/project-a", Branch: "feature", Integration: "dev", Start: "a"}
	b := review.Scope{Repository: "/project-b", Branch: "feature", Integration: "main", Start: "b"}
	// The old global session remembers B; A's prior range survives in its review.
	if err := writeJSON(filepath.Join(dir, "session.json"), b); err != nil {
		t.Fatal(err)
	}
	db, record, err := Open(dir, a)
	if err != nil {
		t.Fatal(err)
	}
	record.Comments = []Comment{NewComment("file.go", "file.go", "A saved note", Anchor{Side: 1, Start: 1, End: 1, Excerpt: "call()"})}
	if err := db.Save(record); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(db.path)
	if branch, err := LastBranch(dir, b.Repository); err != nil || branch != b.Branch {
		t.Fatalf("legacy selection not migrated: %s, %v", branch, err)
	}
	for _, want := range []review.Scope{a, b} {
		got, err := LoadScope(dir, want.Repository, want.Branch)
		if err != nil || got != want {
			t.Fatalf("legacy range not restored: %+v, %v", got, err)
		}
	}
	// Once migrated, loading does not depend on the single old session file.
	os.Remove(filepath.Join(dir, "session.json"))
	for _, want := range []review.Scope{a, b} {
		if got, err := LoadScope(dir, want.Repository, want.Branch); err != nil || got != want {
			t.Fatalf("migrated range was not persisted: %+v, %v", got, err)
		}
	}
	after, _ := os.ReadFile(db.path)
	if string(before) != string(after) {
		t.Fatal("scope migration modified saved comments")
	}
}

func TestScopeMigrationDoesNotGuessBetweenMultipleOldRanges(t *testing.T) {
	dir := t.TempDir()
	a := review.Scope{Repository: "/repo", Branch: "feature", Integration: "dev", Start: "a"}
	b := a
	b.Start = "b"
	for _, scope := range []review.Scope{a, b} {
		db, record, _ := Open(dir, scope)
		if err := db.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadScope(dir, a.Repository, a.Branch); err == nil {
		t.Fatal("ambiguous saved ranges were guessed")
	}
	if err := writeJSON(filepath.Join(dir, "session.json"), b); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadScope(dir, a.Repository, a.Branch); err != nil || got != b {
		t.Fatalf("explicit legacy selection was not preferred: %+v, %v", got, err)
	}
}

func TestConcurrentScopeSavesRetainEveryBranch(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for _, branch := range []string{"one", "two", "three"} {
		wg.Go(func() {
			if err := SaveScope(dir, review.Scope{Repository: "/repo", Branch: branch, Integration: "dev", Start: branch}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, branch := range []string{"one", "two", "three"} {
		if got, err := LoadScope(dir, "/repo", branch); err != nil || got.Start != branch {
			t.Fatalf("concurrent save lost %s: %+v, %v", branch, got, err)
		}
	}
}

func TestCorruptProjectSessionIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	scope := review.Scope{Repository: "/repo", Branch: "feature", Integration: "dev", Start: "a"}
	if err := SaveScope(dir, scope); err != nil {
		t.Fatal(err)
	}
	path := sessionPath(dir, scope.Repository)
	os.WriteFile(path, []byte("broken"), 0o600)
	if err := SaveScope(dir, scope); err == nil {
		t.Fatal("corrupt session was overwritten")
	}
	if data, _ := os.ReadFile(path); string(data) != "broken" {
		t.Fatal("corrupt session changed")
	}
}

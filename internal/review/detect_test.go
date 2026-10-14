package review

import (
	"context"
	"testing"
)

func TestDetectCompleteFeatureAfterMergesBack(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("first", map[string]string{"feature.txt": "one\n"})
	f.git("checkout", "dev")
	f.commit("background", map[string]string{"background.txt": "background\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.commit("second", map[string]string{"feature.txt": "two\n"})
	f.git("checkout", "dev")
	f.git("merge", "--no-ff", "--no-edit", "feature")
	f.commit("more background", map[string]string{"more.txt": "background\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.commit("third", map[string]string{"feature.txt": "three\n"})
	scope, err := DetectScope(context.Background(), f.path, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if scope.Start != start || scope.Integration != "dev" {
		t.Fatalf("wrong detected scope: %+v", scope)
	}
}

func TestDetectSimpleBranchWithoutReflog(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("first", map[string]string{"feature.txt": "one\n"})
	f.commit("second", map[string]string{"feature.txt": "two\n"})
	f.git("reflog", "expire", "--expire=all", "--all")
	scope, err := DetectScope(context.Background(), f.path, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if scope.Start != start {
		t.Fatalf("wrong start: %+v", scope)
	}
}

func TestSnapshotLifetime(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature", map[string]string{"feature.txt": "one\n"})
	result, err := Analyze(context.Background(), Options{Scope: Scope{Repository: f.path, Branch: "feature", Integration: "dev", Start: start}, KeepSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if result.SnapshotPath == "" || result.Baseline == "" {
		t.Fatal("snapshot missing")
	}
	r := gitRepository{path: result.SnapshotPath}
	if _, err := r.run(context.Background(), "cat-file", "-e", result.Baseline); err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
}

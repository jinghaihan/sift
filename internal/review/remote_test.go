package review

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestReviewRemoteTrackingBranchWithoutLocalCheckout(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	f.commit("feature work", map[string]string{"feature.txt": "remote feature\n"})
	f.git("update-ref", "refs/remotes/origin/feature", "HEAD")
	f.git("update-ref", "refs/remotes/origin/dev", "dev")
	f.git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/dev")
	f.git("checkout", "dev")
	f.git("branch", "-D", "feature")
	f.write("shared.txt", "uncommitted work\n")
	before := f.git("status", "--porcelain=v1")

	info, err := Inspect(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(info.Branches, "origin/feature") || slices.Contains(info.Branches, "origin/HEAD") || slices.Contains(info.Branches, "feature") {
		t.Fatalf("incorrect remote branch choices: %v", info.Branches)
	}
	scope, err := DetectScope(context.Background(), f.path, "origin/feature")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Analyze(context.Background(), Options{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Patch, "+remote feature") || strings.Contains(result.Patch, "uncommitted work") {
		t.Fatalf("incorrect remote review: %s", result.Patch)
	}
	if f.git("symbolic-ref", "--short", "HEAD") != "dev" || f.git("status", "--porcelain=v1") != before {
		t.Fatal("remote review modified the source working tree")
	}
}

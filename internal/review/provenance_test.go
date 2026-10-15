package review

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTrackingCheckoutOfMergedFeatureIsNotReportedAsEmpty(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	f.commit("feature", map[string]string{"feature.txt": "FEATURE\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.git("remote", "add", "origin", f.path)
	f.git("update-ref", "refs/remotes/origin/feature", "HEAD")
	f.git("branch", "-D", "feature")
	f.git("checkout", "-b", "feature", "origin/feature")
	_, err := DetectScope(context.Background(), f.path, "feature")
	if err == nil || errors.Is(err, ErrNoFeatureChanges) {
		t.Fatalf("a tracking checkout lost its already-merged feature history: %v", err)
	}
}

func TestRecordedForkSurvivesIntegrationChangingItsFirstParentLine(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "another-feature")
	f.commit("another person's work", map[string]string{"other.txt": "OTHER\n"})
	f.git("checkout", "dev")
	f.commit("iteration work", map[string]string{"upstream.txt": "UPSTREAM\n"})
	f.git("merge", "--no-edit", "another-feature")
	f.git("checkout", "-b", "feature", "HEAD")
	start := f.commit("feature one", map[string]string{"first.txt": "FIRST\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.git("checkout", "another-feature")
	f.git("merge", "--no-edit", "dev")
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "another-feature")
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	f.commit("feature two", map[string]string{"second.txt": "SECOND\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	scope, err := DetectScope(context.Background(), f.path, "feature")
	if err != nil || scope.Start != start {
		t.Fatalf("lost the recorded fork after integration changed its first-parent line: %+v %v", scope, err)
	}
	result, err := Analyze(context.Background(), Options{Scope: scope})
	if err != nil || result.Commits != 2 || !strings.Contains(result.Patch, "+FIRST") || !strings.Contains(result.Patch, "+SECOND") {
		t.Fatalf("lost feature work after the fork left the current integration mainline: %v %+v", err, result)
	}
	for _, path := range []string{"other.txt", "upstream.txt"} {
		if strings.Contains(result.Patch, path) {
			t.Fatalf("unrelated %s leaked into the review", path)
		}
	}
}

func TestRecordedSyncDoesNotFollowAnotherBranchContainingTheFeature(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature one", map[string]string{"first.txt": "FIRST\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("upstream one", map[string]string{"upstream.txt": "UPSTREAM\n"})
	f.git("checkout", "-b", "another-feature")
	f.commit("someone else's feature", map[string]string{"other.txt": "OTHER\n"})
	f.git("checkout", "dev")
	f.commit("more upstream", map[string]string{"more.txt": "MORE\n"})
	f.git("merge", "--no-edit", "another-feature")
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	f.commit("feature two", map[string]string{"second.txt": "SECOND\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("latest upstream", map[string]string{"latest.txt": "LATEST\n"})
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	result := f.analyze(start)
	if !strings.Contains(result.Patch, "+FIRST") || !strings.Contains(result.Patch, "+SECOND") || result.Commits != 2 {
		t.Fatalf("lost the actual feature path: %+v", result)
	}
	for _, path := range []string{"upstream.txt", "other.txt", "more.txt", "latest.txt"} {
		if strings.Contains(result.Patch, path) {
			t.Fatalf("followed another feature through dev: %s", result.Patch)
		}
	}
	f.git("reflog", "expire", "--expire=all", "--all")
	reopened, err := Analyze(context.Background(), Options{Scope: result.Scope, KnownCommits: result.Provenance})
	if err != nil || reopened.Patch != result.Patch {
		t.Fatalf("saved sync anchors did not preserve the actual feature path: %v", err)
	}
}

func TestRemoteTrackingFeatureUsesOnlyItsMatchingLocalDevelopmentLog(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature", map[string]string{"feature.txt": "FEATURE\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("background", map[string]string{"upstream.txt": "UNRELATED\n"})
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	f.git("remote", "add", "origin", f.path)
	f.git("update-ref", "refs/remotes/origin/feature", "HEAD")
	f.git("config", "branch.feature.remote", "origin")
	f.git("config", "branch.feature.merge", "refs/heads/feature")
	scope, err := DetectScope(context.Background(), f.path, "origin/feature")
	if err != nil || scope.Start != start {
		t.Fatalf("remote feature lost its original local history: %+v, %v", scope, err)
	}
	result, err := Analyze(context.Background(), Options{Scope: scope})
	if err != nil || !strings.Contains(result.Patch, "+FEATURE") || strings.Contains(result.Patch, "upstream.txt") {
		t.Fatalf("incorrect remote feature isolation: %v\n%s", err, result.Patch)
	}
}

func TestFeatureCommitsSurviveRepeatedFastForwardIntegration(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("first feature", map[string]string{"shared.txt": strings.Replace(baseText(), "line 1\n", "FEATURE ONE\n", 1)})
	for _, n := range []string{"TWO", "THREE"} {
		f.git("checkout", "dev")
		f.git("merge", "--ff-only", "feature")
		line := "line 7\n"
		if n == "THREE" {
			line = "line 8\n"
		}
		text := strings.Replace(f.git("show", "HEAD:shared.txt")+"\n", line, "UPSTREAM "+n+"\n", 1)
		f.commit("upstream "+n, map[string]string{"shared.txt": text, "upstream-" + n + ".txt": "UNRELATED\n"})
		f.git("checkout", "feature")
		f.git("merge", "--ff-only", "dev")
		f.commit("feature "+n, map[string]string{"feature-" + n + ".txt": "FEATURE " + n + "\n"})
	}
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.git("checkout", "feature")
	f.write("dirty.txt", "unstaged\n")
	f.write("staged.txt", "staged\n")
	f.git("add", "staged.txt")
	before := f.git("status", "--porcelain=v1") + f.git("diff", "--cached") + f.git("rev-parse", "HEAD")

	scope, err := DetectScope(context.Background(), f.path, "feature")
	if err != nil || scope.Start != start {
		t.Fatalf("lost the original feature start after fast-forwards: %+v, %v", scope, err)
	}
	result, err := Analyze(context.Background(), Options{Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"+FEATURE ONE", "+FEATURE TWO", "+FEATURE THREE"} {
		if !strings.Contains(result.Patch, wanted) {
			t.Fatalf("lost %q after merging into dev: %s", wanted, result.Patch)
		}
	}
	if strings.Contains(result.Patch, "+UPSTREAM") || strings.Contains(result.Patch, "-UPSTREAM") || strings.Contains(result.Patch, "upstream-") || result.Commits != 3 {
		t.Fatalf("included imported work or lost feature commits: %+v", result)
	}
	after := f.git("status", "--porcelain=v1") + f.git("diff", "--cached") + f.git("rev-parse", "HEAD")
	if before != after {
		t.Fatal("review modified the source checkout or index")
	}
}

func TestLaterFeatureCommitOnIntegrationMainlineIsNotSilentlySkipped(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature one", map[string]string{"first.txt": "FIRST\n"})
	f.git("checkout", "dev")
	f.commit("unrelated dev", map[string]string{"upstream.txt": "UNRELATED\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.git("checkout", "dev")
	f.git("merge", "--no-ff", "--no-edit", "feature")
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	f.commit("feature two", map[string]string{"second.txt": "SECOND\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	result := f.analyze(start)
	if !strings.Contains(result.Patch, "+FIRST") || !strings.Contains(result.Patch, "+SECOND") || strings.Contains(result.Patch, "upstream.txt") || result.Commits != 2 {
		t.Fatalf("later feature work was treated as upstream: %+v", result)
	}
}

func TestSavedProvenanceSurvivesExpiredReflogs(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature", map[string]string{"feature.txt": "FEATURE\n"})
	original := f.analyze(start)
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("background", map[string]string{"upstream.txt": "UNRELATED\n"})
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	captured, err := Analyze(context.Background(), Options{Scope: original.Scope, KnownCommits: original.Provenance})
	if err != nil {
		t.Fatal(err)
	}
	f.git("reflog", "expire", "--expire=all", "--all")
	options := Options{Scope: original.Scope}
	if _, err := Analyze(context.Background(), options); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("missing provenance should not guess ownership: %v", err)
	}
	options.KnownCommits = original.Provenance
	if _, err := Analyze(context.Background(), options); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("an unseen integration update must not silently drop feature work: %v", err)
	}
	options.KnownCommits = captured.Provenance
	result, err := Analyze(context.Background(), options)
	if err != nil || result.Patch != original.Patch {
		t.Fatalf("saved provenance did not preserve the feature: %v\n%s", err, result.Patch)
	}
}

func TestFeatureChildMergeIsRetainedAfterIntegrationFastForward(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature one", map[string]string{"first.txt": "FIRST\n"})
	f.git("checkout", "-b", "feature-child")
	f.commit("child feature", map[string]string{"child.txt": "CHILD\n"})
	f.git("checkout", "feature")
	f.commit("feature two", map[string]string{"second.txt": "SECOND\n"})
	f.git("merge", "--no-edit", "feature-child")
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("background", map[string]string{"upstream.txt": "UNRELATED\n"})
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	result := f.analyze(start)
	for _, wanted := range []string{"+FIRST", "+SECOND", "+CHILD"} {
		if !strings.Contains(result.Patch, wanted) {
			t.Fatalf("feature-side merge lost %q: %s", wanted, result.Patch)
		}
	}
	if strings.Contains(result.Patch, "upstream.txt") {
		t.Fatal("feature child merge introduced upstream work")
	}
}

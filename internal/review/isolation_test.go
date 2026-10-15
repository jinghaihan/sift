package review

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// The oracle tree is built independently from the original base and only the
// upstream edits. Comparing the entire patch catches missing feature changes
// and imported changes even when they affect the same file or share an author.
func TestIsolationMatchesIndependentBackgroundTree(t *testing.T) {
	for _, schedule := range []struct {
		name  string
		modes []string
	}{
		{"merges", []string{"merge", "merge", "merge"}},
		{"fast-forwards", []string{"ff", "ff", "ff"}},
		{"mixed", []string{"ff", "merge", "ff"}},
	} {
		t.Run(schedule.name, func(t *testing.T) {
			f := newFixture(t)
			base := f.git("rev-parse", "HEAD")
			f.git("checkout", "-b", "feature")
			text := strings.Replace(baseText(), "line 1\n", "FEATURE 1\n", 1)
			start := f.commit("feature one", map[string]string{"shared.txt": text})
			ownIDs := []string{start}
			background := map[string]string{"shared.txt": baseText()}
			for i, mode := range schedule.modes {
				f.git("checkout", "dev")
				if mode == "ff" {
					f.git("merge", "--ff-only", "feature")
				}
				oldLine := fmt.Sprintf("line %d\n", 30+i)
				upstreamLine := fmt.Sprintf("UPSTREAM %d\n", i)
				upstreamPath := fmt.Sprintf("dev-%d.txt", i)
				text = strings.Replace(f.git("show", "HEAD:shared.txt")+"\n", oldLine, upstreamLine, 1)
				f.commit("upstream change by the same author", map[string]string{
					"shared.txt": text, upstreamPath: upstreamLine,
				})
				background["shared.txt"] = strings.Replace(background["shared.txt"], oldLine, upstreamLine, 1)
				background[upstreamPath] = upstreamLine
				f.git("checkout", "feature")
				if mode == "ff" {
					f.git("merge", "--ff-only", "dev")
				} else {
					f.git("merge", "--no-edit", "dev")
				}
				text = strings.Replace(f.git("show", "HEAD:shared.txt")+"\n",
					fmt.Sprintf("line %d\n", 2+i), fmt.Sprintf("FEATURE %d\n", 2+i), 1)
				// Ownership follows the feature history, not an author filter.
				f.git("config", "user.name", fmt.Sprintf("Feature collaborator %d", i))
				f.git("config", "user.email", fmt.Sprintf("collaborator-%d@example.test", i))
				ownIDs = append(ownIDs, f.commit("continue feature", map[string]string{
					"shared.txt": text, fmt.Sprintf("feature-%d.txt", i): "FEATURE\n",
				}))
				f.git("config", "user.name", "Sift Test")
				f.git("config", "user.email", "sift@example.test")
			}
			f.git("checkout", "dev")
			f.git("merge", "--ff-only", "feature")
			// Merging a feature keeps its commit IDs. Their presence on dev
			// must not cause the feature's own changes to disappear.
			for _, id := range ownIDs {
				f.git("merge-base", "--is-ancestor", id, "dev")
			}
			f.git("checkout", "-b", "oracle-background", base)
			expectedTree := f.commit("independent upstream-only oracle", background)
			f.git("checkout", "feature")
			scope, err := DetectScope(context.Background(), f.path, "feature")
			if err != nil || scope.Start != start {
				t.Fatalf("detect original feature start: %+v %v", scope, err)
			}
			result, err := Analyze(context.Background(), Options{Scope: scope, KeepSnapshot: true})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Close()
			assertBackgroundTree(t, f, result, expectedTree)
			if result.Commits != len(ownIDs) || len(result.Uncertain) != 0 {
				t.Fatalf("incorrect ownership or unexpected ambiguity: %+v", result)
			}
			for _, id := range ownIDs {
				found := false
				for _, provenance := range result.Provenance {
					found = found || provenance.ID == id && provenance.Role == featureCommit
				}
				if !found {
					t.Fatalf("feature commit %s was lost after entering dev", id)
				}
			}
			f.git("reflog", "expire", "--expire=all", "--all")
			reopened, err := Analyze(context.Background(), Options{Scope: scope, KnownCommits: result.Provenance})
			if err != nil || reopened.Patch != result.Patch || !reflect.DeepEqual(reopened.Provenance, result.Provenance) {
				t.Fatalf("persisted provenance changed the review after reflog expiry: %v\n%s", err, reopened.Patch)
			}
		})
	}
}

func assertBackgroundTree(t *testing.T, f fixture, result Result, expected string) {
	t.Helper()
	r := gitRepository{path: f.path}
	wanted, err := r.run(context.Background(), "diff", "--no-ext-diff", "--no-color", "--find-renames", "--unified=12", expected, result.Tip, "--")
	if err != nil {
		t.Fatal(err)
	}
	if result.Patch != wanted {
		t.Fatalf("isolated diff differs from independently constructed background\nwant:\n%s\ngot:\n%s", wanted, result.Patch)
	}
	snapshot := gitRepository{path: result.SnapshotPath}
	actual, err := snapshot.run(context.Background(), "rev-parse", result.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(actual) != f.git("rev-parse", expected+"^{tree}") {
		t.Fatal("comparison baseline is not the exact upstream-only tree")
	}
}

func TestFeatureRevertAfterPublicationHasNoNetChanges(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("temporary feature", map[string]string{"feature.txt": "FEATURE\n"})
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	f.commit("upstream", map[string]string{"dev-only.txt": "UPSTREAM\n"})
	f.git("checkout", "feature")
	f.git("merge", "--ff-only", "dev")
	f.git("revert", "--no-edit", start)
	f.git("checkout", "dev")
	f.git("merge", "--ff-only", "feature")
	result := f.analyze(start)
	if result.Patch != "" || len(result.Uncertain) != 0 || result.Commits != 2 {
		t.Fatalf("a reverted feature must cancel out even after its original commit enters dev: %+v", result)
	}
}

func TestFileOperationsMatchIndependentBackgroundTree(t *testing.T) {
	f := newFixture(t)
	base := f.commit("base files", map[string]string{"removed.txt": "REMOVE ME\n"})
	f.git("checkout", "-b", "feature")
	f.git("mv", "shared.txt", "renamed file.txt")
	f.git("rm", "removed.txt")
	start := f.commit("rename, delete and add", map[string]string{
		"renamed file.txt":      strings.Replace(baseText(), "line 1\n", "FEATURE\n", 1),
		"目录/名字 with spaces.txt": "feature without trailing newline",
		"asset.bin":             "\x00\x01\x02\xff",
	})
	f.git("checkout", "dev")
	upstream := strings.Replace(baseText(), "line 30\n", "UPSTREAM\n", 1)
	f.commit("background edits original file", map[string]string{
		"shared.txt": upstream, "dev-only.txt": "UPSTREAM\n",
	})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.git("checkout", "-b", "oracle-background", base)
	expected := f.commit("independent upstream-only oracle", map[string]string{
		"shared.txt": upstream, "dev-only.txt": "UPSTREAM\n",
	})
	f.git("checkout", "feature")
	result, err := Analyze(context.Background(), Options{Scope: Scope{
		Repository: f.path, Branch: "feature", Integration: "dev", Start: start,
	}, KeepSnapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()
	if len(result.Uncertain) != 0 {
		t.Fatalf("non-overlapping file operations became ambiguous: %+v", result.Uncertain)
	}
	assertBackgroundTree(t, f, result, expected)
}

func TestUpstreamDeletionKeepsFeatureConflictMaterial(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature edit", map[string]string{"shared.txt": "FEATURE\n"})
	f.git("checkout", "dev")
	f.git("rm", "shared.txt")
	f.commit("upstream removes file", map[string]string{"dev-only.txt": "UPSTREAM\n"})
	f.git("checkout", "feature")
	if err := exec.Command("git", "-C", f.path, "merge", "--no-edit", "dev").Run(); err == nil {
		t.Fatal("expected modify/delete conflict")
	}
	f.git("rm", "shared.txt")
	resolution := f.commit("accept upstream deletion", nil)
	f.commit("independent feature", map[string]string{"feature.txt": "FINAL FEATURE\n"})
	result := f.analyze(start)
	material := map[string]bool{}
	for _, uncertain := range result.Uncertain {
		if uncertain.Reason == "" || strings.Contains(uncertain.Patch, "dev-only.txt") {
			t.Fatalf("unlabeled conflict or unrelated upstream material: %+v", uncertain)
		}
		material[uncertain.Commit] = true
	}
	if !material[start] || !material[resolution] || !strings.Contains(result.Patch, "+FINAL FEATURE") || strings.Contains(result.Patch, "dev-only.txt") {
		t.Fatalf("feature changes disappeared after upstream deleted their file: %+v", result)
	}
}

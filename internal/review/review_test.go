package review

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type fixture struct {
	t    *testing.T
	path string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{t: t, path: t.TempDir()}
	f.git("init", "-b", "dev")
	f.git("config", "user.name", "Sift Test")
	f.git("config", "user.email", "sift@example.test")
	f.git("config", "commit.gpgSign", "false")
	f.git("config", "core.autocrlf", "false")
	f.commit("base", map[string]string{"shared.txt": baseText()})
	return f
}

func (f fixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.path}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (f fixture) write(path, contents string) {
	f.t.Helper()
	name := filepath.Join(f.path, path)
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) commit(subject string, files map[string]string) string {
	f.t.Helper()
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		f.write(path, files[path])
	}
	f.git("add", "--all")
	f.git("commit", "--quiet", "--allow-empty", "-m", subject)
	return f.git("rev-parse", "HEAD")
}

func baseText() string {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (f fixture) analyze(start string) Result {
	f.t.Helper()
	result, err := Analyze(context.Background(), Options{Scope: Scope{Repository: f.path, Branch: "feature", Integration: "dev", Start: start}})
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

func TestExcludeUpstreamChangesIncludingSameFile(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature A", map[string]string{"shared.txt": strings.Replace(baseText(), "line 1\n", "FEATURE A\n", 1)})
	f.git("checkout", "dev")
	f.commit("upstream", map[string]string{"shared.txt": strings.Replace(baseText(), "line 30\n", "UPSTREAM\n", 1), "upstream.txt": "not part of this review\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.commit("feature B", map[string]string{"feature.txt": "FEATURE B\n"})

	result := f.analyze(start)
	if strings.Contains(result.Patch, "upstream.txt") || strings.Contains(result.Patch, "+UPSTREAM") {
		t.Fatalf("upstream changes leaked into feature diff:\n%s", result.Patch)
	}
	for _, text := range []string{"+FEATURE A", "+FEATURE B"} {
		if !strings.Contains(result.Patch, text) {
			t.Errorf("missing %q:\n%s", text, result.Patch)
		}
	}
	if result.Commits != 2 || result.Merges != 1 || len(result.Uncertain) != 0 {
		t.Fatalf("unexpected provenance: %+v", result)
	}
}

func TestCompleteFeatureSurvivesRepeatedIntegrationAndMergesBack(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("first feature change", map[string]string{"feature.txt": "FIRST\n"})
	f.git("checkout", "dev")
	f.commit("upstream one", map[string]string{"one.txt": "UPSTREAM ONE\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.commit("second feature change", map[string]string{"second.txt": "SECOND\n"})
	f.git("checkout", "dev")
	f.git("merge", "--no-ff", "--no-edit", "feature")
	f.commit("upstream two", map[string]string{"two.txt": "UPSTREAM TWO\n"})
	f.git("checkout", "feature")
	f.git("merge", "--no-edit", "dev")
	f.commit("refine feature", map[string]string{"feature.txt": "FINAL\n"})

	result := f.analyze(start)
	for _, unwanted := range []string{"one.txt", "two.txt", "+FIRST"} {
		if strings.Contains(result.Patch, unwanted) {
			t.Errorf("unexpected %q:\n%s", unwanted, result.Patch)
		}
	}
	if !strings.Contains(result.Patch, "+FINAL") || !strings.Contains(result.Patch, "+SECOND") {
		t.Fatalf("incomplete feature diff:\n%s", result.Patch)
	}
}

func TestRevertedWorkCancelsOut(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("temporary feature", map[string]string{"feature.txt": "temporary\n"})
	f.git("revert", "--no-edit", start)
	result := f.analyze(start)
	if result.Patch != "" || len(result.Uncertain) != 0 {
		t.Fatalf("reverted feature should have no net diff: %+v", result)
	}
}

func TestConflictsAreLabeledWithoutIncludingUnrelatedDevFiles(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature", map[string]string{"shared.txt": "FEATURE\n"})
	f.git("checkout", "dev")
	f.commit("upstream", map[string]string{"shared.txt": "UPSTREAM\n", "dev-only.txt": "do not include\n"})
	f.git("checkout", "feature")
	if err := exec.Command("git", "-C", f.path, "merge", "--no-edit", "dev").Run(); err == nil {
		t.Fatal("fixture must conflict")
	}
	f.commit("resolve overlap", map[string]string{"shared.txt": "RESOLVED\n"})
	f.commit("independent feature", map[string]string{"feature.txt": "FINAL FEATURE\n"})

	result := f.analyze(start)
	if strings.Contains(result.Patch, "dev-only.txt") {
		t.Fatalf("unrelated dev file leaked:\n%s", result.Patch)
	}
	if !strings.Contains(result.Patch, "+FINAL FEATURE") || len(result.Uncertain) < 2 {
		t.Fatalf("missing feature or conflict material: %+v", result)
	}
	for _, uncertain := range result.Uncertain {
		if uncertain.Reason == "" || strings.Contains(uncertain.Patch, "dev-only.txt") {
			t.Fatalf("incorrect conflict provenance: %+v", uncertain)
		}
	}
}

func TestDirtySourceAndIndexRemainUntouched(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	start := f.commit("feature", map[string]string{"with space.txt": "feature\n"})
	f.write("shared.txt", "staged local work\n")
	f.git("add", "shared.txt")
	f.write("shared.txt", "unstaged local work\n")
	f.write("untracked.txt", "untracked local work\n")
	before := f.git("status", "--porcelain=v1") + f.git("diff") + f.git("diff", "--cached") + f.git("rev-parse", "HEAD")
	f.analyze(start)
	after := f.git("status", "--porcelain=v1") + f.git("diff") + f.git("diff", "--cached") + f.git("rev-parse", "HEAD")
	if before != after {
		t.Fatal("review changed source checkout or index")
	}
}

func TestBinaryChangesAndRenames(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	f.git("mv", "shared.txt", "renamed file.txt")
	start := f.commit("rename and binary asset", map[string]string{"asset.bin": "\x00\x01\x02\x03"})
	result := f.analyze(start)
	if !strings.Contains(result.Patch, "rename to renamed file.txt") || !strings.Contains(result.Patch, "Binary files") {
		t.Fatalf("missing rename or binary change:\n%s", result.Patch)
	}
}

func TestInvalidScopeAndCancellation(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	f.commit("feature", map[string]string{"feature.txt": "feature\n"})
	f.git("checkout", "dev")
	foreign := f.commit("not on feature", map[string]string{"other.txt": "other\n"})
	if _, err := Analyze(context.Background(), Options{Scope: Scope{Repository: f.path, Branch: "feature", Integration: "dev", Start: foreign}}); err == nil {
		t.Fatal("accepted a start outside the feature history")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Analyze(ctx, Options{Scope: Scope{Repository: f.path, Branch: "feature", Integration: "dev", Start: foreign}}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

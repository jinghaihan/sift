package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileExclusionGlobPaths(t *testing.T) {
	for _, test := range []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.md", "README.md", true},
		{"*.md", "README.md", true},
		{"*.md", "docs/deep/intro.md", true},
		{"*.md", "src/file.ts", false},
		{"**/*.md", "docs/guide/intro.md", true},
		{"**/*.md", "docs/intro.mdx", false},
		{"**/*.md", "src/md.ts", false},
		{"**/*.map", "notes.map", true},
		{"**/*.map", "src/app.js.map", true},
		{"src/generated/**", "src/generated/index.ts", true},
		{"src/generated/**", "src/generated/deep/index.ts", true},
		{"src/generated/**", "src/generated", true},
		{"src/generated/**", "other/src/generated/index.ts", false},
		{"docs/*.md", "docs/deep/intro.md", false},
		{"docs/", "docs/deep/intro.md", true},
		{"**/test?.[jt]s", "src/test1.ts", true},
		{"**/test?.[jt]s", "src/test11.ts", false},
		{"/README.md", "README.md", true},
	} {
		glob, err := compilePathGlob(test.pattern)
		if err != nil || glob.matches(test.name) != test.want {
			t.Errorf("%s matches %s: want %v, err %v", test.pattern, test.name, test.want, err)
		}
	}
	for _, invalid := range []string{"", "[broken", "foo\\"} {
		if _, err := compilePathGlob(invalid); err == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
}

func TestFileExclusionSettingsKeepDefaultsAndExplicitEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sift.jsonc")
	os.WriteFile(path, []byte(`{"settings":{"wordWrap":true}}`), 0o600)
	c := &config{path: path, settings: defaultSettings()}
	if !c.load() || len(c.Get().FileExclusions) != 1 || c.Get().FileExclusions[0].Pattern != "*.md" {
		t.Fatal("existing settings without rules did not get the initial default")
	}
	copy := c.Get()
	copy.FileExclusions[0].Pattern = "**/*.map"
	if c.Get().FileExclusions[0].Pattern != "*.md" {
		t.Fatal("settings snapshot mutated shared configuration")
	}
	c.Update(func(s *Settings) { s.FileExclusions = []fileExclusion{} })
	reloaded := &config{path: path, settings: defaultSettings()}
	if !reloaded.load() || len(reloaded.Get().FileExclusions) != 0 {
		t.Fatal("cleared rules were replaced by defaults after restart")
	}
}

func newFileExclusionReview(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	withSettings(t, func(s *Settings) { s.FileExclusions = defaultSettings().FileExclusions })
	w, tt := newFeatureReview(t)
	for name, text := range map[string]string{
		"README.md":              "# Prepared review\nExplain the design.\n",
		"docs/guide.md":          "# Guide\n",
		"notes.map":              "generated mapping\n",
		"src/generated/index.ts": "export const generated = true;\n",
	} {
		writeFile(t, w.repo.Root, name, text)
	}
	gitIn(t, w.repo.Root, "add", ".")
	gitIn(t, w.repo.Root, "commit", "-qm", "feature documentation and generated files")
	w.load()
	if w.loadErr != nil {
		t.Fatal(w.loadErr)
	}
	tt.Frame()
	return w, tt
}

func showsFile(w *window, path string) bool {
	return slices.ContainsFunc(w.rows, func(r row) bool { return r.kind == rowHeader && w.files[r.file].Path == path })
}

func TestFileExclusionsEditorAndTemporaryVisibility(t *testing.T) {
	w, tt := newFileExclusionReview(t)
	patch := w.featureResult.Patch
	if w.hiddenFileCount() != 2 || showsFile(w, "README.md") || showsFile(w, "docs/guide.md") {
		t.Fatal("default rule did not hide root and nested Markdown files")
	}
	if tt.HasText("README.md") {
		t.Fatal("excluded file remained in the file tree")
	}
	if err := tt.Click("File exclusions"); err != nil {
		t.Fatal(err)
	}
	snapshot(t, tt, "file-exclusions")
	if err := tt.Click("Show all files temporarily"); err != nil {
		t.Fatal(err)
	}
	if !showsFile(w, "README.md") || w.hiddenFileCount() != 0 || !w.settings.FileExclusions[0].Enabled {
		t.Fatal("show all changed the configured rule or failed to reveal files")
	}
	tt.Click("Show all files temporarily")
	tt.Click("Exclude pattern 1")
	tt.Command("selectAll")
	tt.Type("**/*.map")
	tt.Click("Apply")
	if !showsFile(w, "README.md") || showsFile(w, "notes.map") || cfg.Get().FileExclusions[0].Pattern != "**/*.map" {
		t.Fatal("editing the rule did not apply and save")
	}
	tt.Click("File exclusions")
	tt.Click("Enable rule 1")
	tt.Click("Add rule")
	tt.Click("Exclude pattern 2")
	tt.Type("[")
	tt.Click("Apply")
	if !w.exclusionsOpen || !tt.HasText("Invalid glob pattern.") {
		t.Fatal("invalid rule was applied")
	}
	tt.Command("selectAll")
	tt.Click("Exclude pattern 2")
	tt.Command("selectAll")
	tt.Type("src/generated/**")
	tt.Click("Apply")
	if !showsFile(w, "notes.map") || showsFile(w, "src/generated/index.ts") || w.settings.FileExclusions[0].Enabled {
		t.Fatal("disabled rules or custom directory rules did not apply")
	}
	tt.Click("File exclusions")
	tt.Click("Delete rule 1")
	tt.Click("Delete rule 1")
	tt.Click("Apply")
	if len(cfg.Get().FileExclusions) != 0 || w.hiddenFileCount() != 0 || !showsFile(w, "src/generated/index.ts") {
		t.Fatal("clearing rules left files hidden")
	}
	if w.featureResult.Patch != patch {
		t.Fatal("display rules changed the computed feature diff")
	}
}

func TestExcludedCommentCanRevealOnlyItsFile(t *testing.T) {
	w, tt := newFileExclusionReview(t)
	w.addComment("README.md", sideNew, 1, 1)
	w.comments[0].text = "Discuss the design during review."
	w.saveFeatureReview()
	tt.Frame()
	if err := tt.Click("Review comments (1)"); err != nil {
		t.Fatal(err)
	}
	if !showsFile(w, "README.md") || showsFile(w, "docs/guide.md") || w.showAllFiles || w.hiddenFileCount() != 1 {
		t.Fatal("comment navigation did not temporarily reveal only its own excluded file")
	}
	if !strings.Contains(w.commentsMarkdown(), "Discuss the design") {
		t.Fatal("filtered comments were omitted from export")
	}
	snapshot(t, tt, "excluded-comment-revealed")
	if err := tt.Click("File exclusions"); err != nil {
		t.Fatal(err)
	}
	tt.Click("Apply")
	if showsFile(w, "README.md") || len(w.comments) != 1 {
		t.Fatal("hiding the revealed file deleted its comment")
	}
	w.load()
	tt.Frame()
	if len(w.comments) != 1 || w.comments[0].outdated {
		t.Fatal("hidden comment did not survive refresh")
	}
}

func TestAllExcludedFilesHaveShowAllAction(t *testing.T) {
	w, tt := newFileExclusionReview(t)
	s := w.settings
	s.FileExclusions = []fileExclusion{{Pattern: "**", Enabled: true}}
	w.applySettings(s)
	tt.Frame()
	if !tt.HasText("All files are hidden") {
		t.Fatal("empty filtered review did not explain why")
	}
	if err := tt.Click("Show all files"); err != nil {
		t.Fatal(err)
	}
	if !showsFile(w, "README.md") || w.hiddenFileCount() != 0 {
		t.Fatal("show all did not recover the diff")
	}
}

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

func TestFileExclusionsEditInlineImmediately(t *testing.T) {
	w, tt := newFileExclusionReview(t)
	patch := w.featureResult.Patch
	if w.hiddenFileCount() != 2 || showsFile(w, "README.md") || showsFile(w, "docs/guide.md") {
		t.Fatal("default rule did not hide root and nested Markdown files")
	}
	if err := tt.Click("File exclusions"); err != nil {
		t.Fatal(err)
	}
	input, ok := tt.Find("Files to exclude")
	search, _ := tt.Find("Filter files")
	if !ok || input.Y <= search.Y || input.X > search.X || input.X < 8 || input.W < search.W {
		t.Fatalf("exclusion input is not below the file search: input=%+v search=%+v", input, search)
	}
	if tt.HasText("Apply") || tt.HasText("Cancel") || tt.HasText("Add rule") || tt.HasText("Show all files temporarily") {
		t.Fatal("old popover controls remained")
	}
	tt.Click("Files to exclude")
	tt.Command("selectAll")
	tt.Type("*.md, *.map, src/generated/**")
	if showsFile(w, "README.md") || showsFile(w, "notes.map") || showsFile(w, "src/generated/index.ts") || len(cfg.Get().FileExclusions) != 3 {
		t.Fatal("comma-separated exclusions did not apply and save immediately")
	}
	snapshot(t, tt, "file-exclusions-inline")
	tt.Click("File exclusions")
	if _, ok := tt.Find("Files to exclude"); ok || w.hiddenFileCount() != 4 {
		t.Fatal("collapsing the field disabled filtering or left the input visible")
	}
	tt.Click("File exclusions")
	if w.exclusionsText != "*.md, *.map, src/generated/**" {
		t.Fatal("reopening changed the input")
	}
	tt.Click("Files to exclude")
	tt.Command("selectAll")
	tt.Type("*.md, [")
	if w.exclusionsError == "" || len(cfg.Get().FileExclusions) != 1 || showsFile(w, "README.md") {
		t.Fatal("invalid draft was saved or prevented valid patterns from applying")
	}
	tt.Click("Clear exclusions")
	if len(cfg.Get().FileExclusions) != 0 || w.hiddenFileCount() != 0 || !showsFile(w, "src/generated/index.ts") {
		t.Fatal("clearing the field left files hidden")
	}
	if w.featureResult.Patch != patch {
		t.Fatal("display rules changed the computed feature diff")
	}
	cfg.flushFileExclusions()
	reloaded := &config{path: cfg.path, settings: defaultSettings()}
	if !reloaded.load() || len(reloaded.Get().FileExclusions) != 0 {
		t.Fatal("cleared exclusions did not persist after restart")
	}
}

func TestCommaSeparatedExclusionsKeepLiteralCommas(t *testing.T) {
	rules, invalid := parseFileExclusions(` *.md, , report\,old.txt, file[,]name, src/generated/** `)
	want := []fileExclusion{{"*.md", true}, {`report\,old.txt`, true}, {"file[,]name", true}, {"src/generated/**", true}}
	if invalid != "" || !slices.Equal(rules, want) {
		t.Fatalf("parsed %+v, error %q", rules, invalid)
	}
	for _, pattern := range rules[1:3] {
		glob, err := compilePathGlob(pattern.Pattern)
		name := "report,old.txt"
		if pattern.Pattern == "file[,]name" {
			name = "file,name"
		}
		if err != nil || !glob.matches(name) {
			t.Fatalf("literal comma failed: %+v, %v", pattern, err)
		}
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
	if !showsFile(w, "README.md") || showsFile(w, "docs/guide.md") || w.hiddenFileCount() != 1 {
		t.Fatal("comment navigation did not temporarily reveal only its own excluded file")
	}
	if !strings.Contains(w.commentsMarkdown(), "Discuss the design") {
		t.Fatal("filtered comments were omitted from export")
	}
	snapshot(t, tt, "excluded-comment-revealed")
	if err := tt.Click("File exclusions"); err != nil {
		t.Fatal(err)
	}
	tt.Click("Clear exclusions")
	tt.Click("Files to exclude")
	tt.Type("*.md")
	if showsFile(w, "README.md") || len(w.comments) != 1 {
		t.Fatal("hiding the revealed file deleted its comment")
	}
	w.load()
	tt.Frame()
	if len(w.comments) != 1 || w.comments[0].outdated {
		t.Fatal("hidden comment did not survive refresh")
	}
}

func TestAllExcludedFilesCanBeRecoveredByClearingInput(t *testing.T) {
	w, tt := newFileExclusionReview(t)
	s := w.settings
	s.FileExclusions = []fileExclusion{{Pattern: "**", Enabled: true}}
	w.applySettings(s)
	tt.Frame()
	if !tt.HasText("All files are hidden") {
		t.Fatal("empty filtered review did not explain why")
	}
	if tt.HasText("Show all files") {
		t.Fatal("removed visibility feature remained in the empty review")
	}
	if err := tt.Click("File exclusions"); err != nil {
		t.Fatal(err)
	}
	tt.Click("Clear exclusions")
	if !showsFile(w, "README.md") || w.hiddenFileCount() != 0 {
		t.Fatal("clearing the input did not recover the diff")
	}
}

func TestLiveExclusionsSaveLatestAndPreserveWindowAppearance(t *testing.T) {
	c := &config{path: filepath.Join(t.TempDir(), "sift.jsonc"), settings: defaultSettings()}
	w := &window{settings: defaultSettings()}
	w.settings.DiffStyle, w.settings.WordWrap = "unified", true
	off := c.OnChange(w.applySettings)
	defer off()
	// Persistence may be busy, but edits and notifications must remain immediate.
	c.writeMu.Lock()
	c.SetFileExclusions([]fileExclusion{{Pattern: "*.map", Enabled: true}})
	got := c.Get()
	c.writeMu.Unlock()
	if got.FileExclusions[0].Pattern != "*.map" || w.settings.DiffStyle != "unified" || !w.settings.WordWrap {
		t.Fatal("live edits waited for disk or reset the window's appearance")
	}
	for _, pattern := range []string{"*", "*.", "*.m", "*.md"} {
		c.SetFileExclusions([]fileExclusion{{Pattern: pattern, Enabled: true}})
	}
	c.flushFileExclusions()
	reloaded := &config{path: c.path, settings: defaultSettings()}
	if !reloaded.load() || reloaded.Get().FileExclusions[0].Pattern != "*.md" {
		t.Fatal("a late save replaced the latest typed rule")
	}
}

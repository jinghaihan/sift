package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
)

func TestFeatureSidebarCanReopen(t *testing.T) {
	for _, kind := range []sourceKind{sourceFeature, sourceHistorical} {
		for _, position := range []string{"left", "right"} {
			t.Run(fmt.Sprintf("%d/%s", kind, position), func(t *testing.T) {
				w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: kind, ref: "feature"})
				w.settings.SidebarPosition = position
				w.sidebarShown, w.sidebarWidth = true, sidebarDefault
				tt := ui.NewTester(w.view, 1280, 860)
				tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78})
				for range 2 {
					if err := tt.Click("Hide sidebar (⌘⇧B)"); err != nil {
						t.Fatal(err)
					}
					button, ok := tt.Find("Show sidebar (⌘⇧B)")
					if w.sidebarShown || !ok {
						t.Fatal("collapsed sidebar has no reopen button")
					}
					if button.X < 78 {
						t.Fatalf("reopen button overlaps window controls: %+v", button)
					}
					if err := tt.Click("Show sidebar (⌘⇧B)"); err != nil {
						t.Fatal(err)
					}
					if !w.sidebarShown {
						t.Fatal("sidebar did not reopen")
					}
				}
			})
		}
	}
}

func TestFeatureRangeUsesNativeViewerAndPersistsComments(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "main.go", mainGo)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "checkout", "-qb", "dev")
	writeFile(t, dir, "dev-only.txt", "background\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "background")
	gitIn(t, dir, "checkout", "-qb", "feature", "main")
	writeFile(t, dir, "main.go", strings.Replace(mainGo, "Hello", "Hi", 1))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "feature")
	gitIn(t, dir, "merge", "--no-edit", "dev")
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(repo, source{kind: sourceFeature, ref: "feature"})
	w.load()
	if w.loadErr != nil {
		t.Fatal(w.loadErr)
	}
	defer w.featureResult.Close()
	if len(w.files) != 1 || w.files[0].Path != "main.go" {
		t.Fatalf("background leaked: %+v", w.files)
	}
	if !w.files[0].canExpand() {
		t.Fatal("native viewer cannot expand snapshot context")
	}
	tt := ui.NewTester(w.view, 1280, 860)
	if !tt.HasText("main.go") {
		t.Fatal("native file tree did not render")
	}
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Handle empty names?"
	w.saveFeatureReview()
	w.setViewed(w.files[0], true)
	second := newWindow(repo, source{kind: sourceFeature, ref: "feature"})
	second.load()
	if second.loadErr != nil {
		t.Fatal(second.loadErr)
	}
	defer second.featureResult.Close()
	if len(second.comments) != 1 || second.comments[0].text != "Handle empty names?" {
		t.Fatalf("comments not restored: %+v", second.comments)
	}
	if !second.isViewed(second.files[0]) {
		t.Fatal("viewed state not restored")
	}
	if !strings.Contains(second.settings.CodeFontFamily, "Geist") {
		t.Fatal("code font does not prefer Geist")
	}
}

// Optional local acceptance check; no private repository paths are committed.
func TestLocalFeatureScope(t *testing.T) {
	path, branch := os.Getenv("SIFT_TEST_REPOSITORY"), os.Getenv("SIFT_TEST_BRANCH")
	if path == "" || branch == "" {
		t.Skip("set a local repository and branch to inspect")
	}
	scope, err := review.DetectScope(t.Context(), path, branch)
	if err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("SIFT_TEST_START"); expected != "" && !strings.HasPrefix(scope.Start, expected) {
		t.Fatalf("scope: %+v", scope)
	}
	t.Logf("%s · %s · %s", filepath.Base(scope.Repository), scope.Integration, scope.Start[:9])
}

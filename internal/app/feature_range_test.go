package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
)

func TestEmptyBranchClearsPreviousFeatureAndPreservesItsReview(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep this review on the original feature."
	w.saveFeatureReview()
	gitIn(t, w.repo.Root, "branch", "empty-feature", "main")
	w.featureBranch = "empty-feature"
	w.featureScope = review.Scope{}
	w.setSource(source{kind: sourceFeature, ref: "empty-feature"})
	tt.Frame()
	if !errors.Is(w.loadErr, review.ErrNoFeatureChanges) || w.featureNeedsScope || !tt.HasText("No feature changes") {
		t.Fatalf("an empty feature should not ask for history selection: %v", w.loadErr)
	}
	if len(w.files) != 0 || len(w.comments) != 0 || w.featureDB != nil {
		t.Fatal("empty branch retained the previous feature's files or comments")
	}
	w.featureBranch = "feature"
	w.setSource(source{kind: sourceFeature, ref: "feature"})
	if w.loadErr != nil || len(w.comments) != 1 || w.comments[0].text != "Keep this review on the original feature." {
		t.Fatal("switching through an empty branch lost the original review")
	}
}

func TestFeatureRangeSelectsBranchesAndCommitsWithoutTypingHashes(t *testing.T) {
	w := newWindow(&git.Repo{Root: filepath.Join(t.TempDir(), "example")}, source{kind: sourceFeature, ref: "origin/feature/search"})
	w.featureBranch, w.hold = "origin/feature/search", true
	state := &featureRangeState{branches: []string{"dev202610", "origin/dev20261113"}}
	for i := range 400 {
		state.commits = append(state.commits, review.Commit{
			ID: fmt.Sprintf("%08x%032x", 0xc31a9dc0+i, i+1), Subject: fmt.Sprintf("feat: improve search %03d", i), Date: "2026-10-15",
		})
	}
	w.featureRange, w.featureNeedsScope = state, true
	w.loadErr = fmt.Errorf("cannot infer the first feature commit")
	tt := ui.NewTester(w.view, 1280, 860)
	if err := tt.Click("Choose integration branch"); err != nil {
		t.Fatal(err)
	}
	tt.Type("origin/dev")
	tt.Key(0, ui.KeyEnter)
	if state.integration != "origin/dev20261113" {
		t.Fatalf("branch selection did not commit its choice: %q", state.integration)
	}
	if err := tt.Click("Choose first feature commit"); err != nil {
		t.Fatal(err)
	}
	firstChoice := state.commits[0].ShortID() + " · " + state.commits[0].Subject
	row, ok := tt.Find(firstChoice)
	if !ok {
		t.Fatal("built-in combobox did not open commit choices")
	}
	x, y := row.X+20, row.Y+row.H/2
	tt.Move(x, y)
	for range 50 {
		tt.Scroll(x, y, 0, 500)
	}
	last := state.commits[len(state.commits)-1]
	lastChoice := last.ShortID() + " · " + last.Subject
	lastRow, ok := tt.Find(lastChoice)
	if !ok || lastRow.Y < 0 || lastRow.Y+lastRow.H > 860 {
		t.Fatalf("last commit is not reachable: %+v", lastRow)
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("Choose first feature commit"); err != nil {
		t.Fatal(err)
	}
	tt.Type("search 007")
	first := state.commits[7]
	choice := first.ShortID() + " · " + first.Subject
	if !tt.HasText(choice) || tt.HasText(lastChoice) {
		t.Fatal("commit search did not filter by subject")
	}
	snapshot(t, tt, "feature-history-builtin-search")
	tt.Key(0, ui.KeyEnter)
	if state.start != first.ID || w.featureScope.Start != "" {
		t.Fatal("selection should preview the range before starting review")
	}
	snapshot(t, tt, "feature-history-builtin-light")
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "feature-history-builtin-dark")
	tt.SetDark(false)
	tt.Frame()
	if err := tt.Click("Review feature"); err != nil {
		t.Fatal(err)
	}
	if w.featureRange != nil || w.featureScope.Start != first.ID || w.featureScope.Integration != state.integration || len(w.held) != 1 {
		t.Fatalf("starting review did not apply the chosen history exactly once: state=%v scope=%+v jobs=%d", w.featureRange, w.featureScope, len(w.held))
	}
}

func TestFeatureHistorySelectionOnlyAppearsAfterInferenceFailure(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureRange = &featureRangeState{loading: true}
	tt := ui.NewTester(w.view, 1100, 760)
	if _, exists := tt.Find("Feature history selection"); exists {
		t.Fatal("normal review offered manual history selection")
	}
	w.loadErr = fmt.Errorf("could not read a file")
	tt.Frame()
	if _, exists := tt.Find("Feature history selection"); exists {
		t.Fatal("a generic load failure offered manual history selection")
	}
	w.featureNeedsScope = true
	tt.Frame()
	if _, exists := tt.Find("Feature history selection"); !exists {
		t.Fatal("an inference failure did not offer inline history selection")
	}
}

func TestFeatureRangeIgnoresObsoleteLoad(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.prepareFeatureHistory()
	old, job := w.featureRange, w.held[0]
	w.featureRange = nil
	job()
	if !old.loading {
		t.Fatal("an obsolete history selection accepted a background result")
	}
}

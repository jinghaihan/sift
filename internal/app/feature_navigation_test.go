package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
)

func TestFeatureBranchPickerReloadsRefsOnEachOpen(t *testing.T) {
	w, tt := newFeatureReview(t)
	gitIn(t, w.repo.Root, "branch", "new-feature")
	if err := tt.Click("Feature branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Contains(w.featureBranches, "new-feature") {
		t.Fatal("opening the branch picker did not read a newly created branch")
	}
	tt.Key(0, ui.KeyEscape)
	gitIn(t, w.repo.Root, "branch", "-D", "new-feature")
	gitIn(t, w.repo.Root, "update-ref", "refs/remotes/origin/another", "HEAD")
	if err := tt.Click("Feature branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if slices.Contains(w.featureBranches, "new-feature") || !slices.Contains(w.featureBranches, "origin/another") {
		t.Fatal("reopening the picker did not update local and remote choices")
	}
	if w.featureBranch != "feature" || strings.TrimSpace(gitIn(t, w.repo.Root, "branch", "--show-current")) != "feature" {
		t.Fatal("refreshing choices changed the reviewed or checked-out branch")
	}
}

func TestFeatureBranchSearchAndLongListScrolling(t *testing.T) {
	w := newWindow(&git.Repo{Root: filepath.Join(t.TempDir(), "example")}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.loadErr = review.ErrNoFeatureChanges
	for i := range 400 {
		w.featureBranches = append(w.featureBranches, fmt.Sprintf("feature/%04d", i))
	}
	tt := ui.NewTester(w.view, 1280, 860)
	snapshot(t, tt, "feature-empty-godiff")
	if err := tt.Click("Feature branch"); err != nil {
		t.Fatal(err)
	}
	snapshot(t, tt, "branch-builtin-open-godiff")
	queued := len(w.held)
	first, ok := tt.Find("feature/0000")
	if !ok {
		t.Fatal("branch list did not open")
	}
	tt.Move(first.X+20, first.Y+10)
	tt.Scroll(first.X+20, first.Y+10, 0, 20000)
	snapshot(t, tt, "branch-builtin-scroll")
	last, ok := tt.Find("feature/0399")
	if !ok || last.Y < 52 || last.Y+last.H > first.Y+280 {
		t.Fatalf("last branch is not reachable inside a bounded popup: first=%+v last=%+v", first, last)
	}
	if w.featureBranch != "feature" || len(w.held) != queued {
		t.Fatal("scrolling changed the branch")
	}
	// Typing filters the choices without starting a review until selection.
	tt.Command("selectAll")
	tt.Type("0399")
	if w.featureBranch != "feature" || len(w.held) != queued || !tt.HasText("feature/0399") {
		t.Fatal("branch search changed the review before confirmation")
	}
	snapshot(t, tt, "branch-builtin-search")
	tt.Key(0, ui.KeyEnter)
	if w.source.ref != "feature/0399" || len(w.held) != queued+1 {
		t.Fatal("Enter did not select the matching branch")
	}
}

func TestHistoricalChangesUseCommandPaletteStyle(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.featureResult = &review.Result{Scope: review.Scope{Branch: "feature"}}
	for i := range 30 {
		w.featureResult.Uncertain = append(w.featureResult.Uncertain, review.Uncertain{
			Commit: fmt.Sprintf("%040x", i+1), Subject: fmt.Sprintf("Feature change %02d", i), Reason: "Merge resolution · historical changes",
		})
	}
	tt := ui.NewTester(w.view, 1280, 860)
	if err := tt.Click("30 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	panel, ok := tt.Find("Historical changes picker")
	if !ok || panel.W != 560 || panel.X != (1280-panel.W)/2 || panel.Y != 120 {
		t.Fatalf("history did not reuse the centered command palette surface: %+v", panel)
	}
	if !tt.Focused("Search historical changes") {
		t.Fatal("history search did not receive keyboard focus")
	}
	list, ok := tt.Find("Historical changes picker choices")
	if !ok || list.Y+list.H > 580 {
		t.Fatalf("historical list is not bounded: %+v", list)
	}
	if panel.X+panel.W-(list.X+list.W) != 1 {
		t.Fatalf("historical scrollbar is inset from the panel edge: panel=%+v list=%+v", panel, list)
	}
	tt.Move(list.X+list.W-4, list.Y+40)
	snapshot(t, tt, "historical-palette-scrollbar")
	tt.Scroll(list.X+20, list.Y+40, 0, 2000)
	last := w.featureResult.Uncertain[29]
	if _, ok := tt.Find("Historical patch " + last.Commit); !ok {
		t.Fatal("historical list cannot scroll to its last patch")
	}
	if err := tt.Click("Search historical changes"); err != nil {
		t.Fatal(err)
	}
	tt.Type("change 29")
	if !tt.HasText(last.Subject) || tt.HasText("Feature change 00") {
		t.Fatal("historical search did not filter patches")
	}
	snapshot(t, tt, "historical-palette-search")
	tt.Key(0, ui.KeyEnter)
	if w.featureHistoryOpen || w.source.kind != sourceHistorical || w.source.ref != last.Commit {
		t.Fatal("choosing a historical patch did not close the picker and open its diff")
	}
}

func TestHistoricalPaletteKeyboardNavigationAndEscape(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.featureResult = &review.Result{Scope: review.Scope{Branch: "feature"}, Uncertain: []review.Uncertain{
		{Commit: strings.Repeat("a", 40), Subject: "Same subject", Reason: "Merge resolution"},
		{Commit: strings.Repeat("b", 40), Subject: "Same subject", Reason: "Overlapping changes"},
	}}
	tt := ui.NewTester(w.view, 1280, 860)
	if err := tt.Click("2 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if w.historicalRow != 1 {
		t.Fatal("Down did not select the next historical patch")
	}
	tt.Key(0, ui.KeyUp)
	if w.historicalRow != 0 {
		t.Fatal("Up did not select the previous historical patch")
	}
	tt.Key(0, ui.KeyEscape)
	if w.featureHistoryOpen || w.source.kind != sourceFeature {
		t.Fatal("Escape did not dismiss the picker without changing the review")
	}
	if err := tt.Click("2 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	w.paletteOpen = true
	tt.Frame()
	if w.featureHistoryOpen {
		t.Fatal("command bar stacked a second modal over historical changes")
	}
	tt.Key(0, ui.KeyEscape)
	if err := tt.Click("2 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if w.featureHistoryOpen || w.source.ref != strings.Repeat("b", 40) {
		t.Fatal("keyboard selection opened the wrong patch with a duplicate subject")
	}
}

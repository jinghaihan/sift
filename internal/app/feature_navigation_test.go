package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
)

func TestFeatureBranchSearchAndLongListScrolling(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	for i := range 400 {
		w.featureBranches = append(w.featureBranches, fmt.Sprintf("feature/%04d", i))
	}
	tt := ui.NewTester(w.view, 1280, 860)
	if err := tt.Click("Feature branch"); err != nil {
		t.Fatal(err)
	}
	first, ok := tt.Find("feature/0000")
	if !ok {
		t.Fatal("branch list did not open")
	}
	tt.Move(first.X+20, first.Y+10)
	tt.Scroll(first.X+20, first.Y+10, 0, 20000)
	snapshot(t, tt, "branch-scroll")
	last, ok := tt.Find("feature/0399")
	if !ok || last.Y < 52 || last.Y+last.H > 400 {
		t.Fatalf("last branch is not reachable inside a bounded popup: first=%+v last=%+v", first, last)
	}
	if w.featureBranch != "feature" || len(w.held) != 0 {
		t.Fatal("scrolling changed the branch")
	}
	// Typing filters the choices without starting a review until selection.
	tt.Command("selectAll")
	tt.Type("0399")
	if w.featureBranch != "feature" || len(w.held) != 0 || !tt.HasText("feature/0399") {
		t.Fatal("branch search changed the review before confirmation")
	}
	snapshot(t, tt, "branch-search")
	tt.Key(0, ui.KeyEnter)
	if w.source.ref != "feature/0399" || len(w.held) == 0 {
		t.Fatal("Enter did not select the matching branch")
	}
}

func TestHistoricalChangesPopoverUsesToolbarAnchor(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.featureResult = &review.Result{Scope: review.Scope{Branch: "feature"}}
	for i := range 30 {
		w.featureResult.Uncertain = append(w.featureResult.Uncertain, review.Uncertain{
			Commit: fmt.Sprintf("%040x", i+1), Subject: fmt.Sprintf("Feature change %02d", i), Reason: "Merge resolution · historical changes",
		})
	}
	tt := ui.NewTester(w.view, 1280, 860)
	trigger, _ := tt.Find("30 historical conflict patches")
	if err := tt.Click("30 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	heading, ok := tt.Find("Historical changes")
	if !ok || heading.Y < trigger.Y+trigger.H || heading.Y > trigger.Y+trigger.H+40 {
		t.Fatalf("history did not open beneath the toolbar trigger: %+v, %+v", trigger, heading)
	}
	list, ok := tt.Find("Historical patches")
	if !ok || list.Y+list.H > 580 {
		t.Fatalf("historical list is not bounded: %+v", list)
	}
	panel, ok := tt.Find("Historical changes popover")
	if !ok || panel.X+panel.W-(list.X+list.W) != 1 {
		t.Fatalf("historical scrollbar is inset from the panel edge: panel=%+v list=%+v", panel, list)
	}
	tt.Move(list.X+list.W-4, list.Y+40)
	snapshot(t, tt, "historical-scrollbar")
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
	snapshot(t, tt, "historical-popover")
	if err := tt.Click("Historical patch " + last.Commit); err != nil {
		t.Fatal(err)
	}
	if w.featureHistoryOpen || w.source.kind != sourceHistorical || w.source.ref != last.Commit {
		t.Fatal("choosing a historical patch did not close the popover and open its diff")
	}
}

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
)

func TestConflictOnlyFeatureDoesNotLookEmpty(t *testing.T) {
	branch := "feature/search"
	w := newWindow(&git.Repo{Root: filepath.Join(t.TempDir(), "example")}, source{kind: sourceFeature, ref: branch})
	w.featureBranch, w.hold = branch, true
	w.featureResult = &review.Result{Scope: review.Scope{Branch: branch}, Uncertain: []review.Uncertain{
		{Commit: strings.Repeat("e41dd59b", 5), Subject: "fix: preserve search behavior after merging dev", Reason: "Merge resolution · historical changes", Patch: "historical patch"},
		{Commit: strings.Repeat("28dfe999", 5), Subject: "feat: keep the query when navigating back", Reason: "Overlapping changes · historical feature patch", Patch: "historical patch"},
		{Commit: strings.Repeat("6199be58", 5), Subject: "fix: retain keyboard navigation on refresh", Reason: "Overlapping changes · historical feature patch", Patch: "historical patch"},
	}}
	tt := ui.NewTester(w.view, 1100, 760)
	if !tt.HasText("Review historical changes from the warning menu above.") || tt.HasText("No local changes") {
		t.Fatal("conflict-only changes were presented as an empty feature")
	}
	if !tt.HasText("Historical changes") || tt.HasText(w.featureResult.Uncertain[0].Subject) {
		t.Fatal("the main view duplicates the warning menu's history list")
	}
	snapshot(t, tt, "conflict-only-godiff-empty")
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "conflict-only-godiff-empty-dark")
	tt.SetDark(false)
	tt.Frame()
	if err := tt.Click("3 historical conflict patches"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Historical changes picker"); !ok || !tt.HasText(w.featureResult.Uncertain[1].Subject) {
		t.Fatal("warning menu did not show the historical changes")
	}
	snapshot(t, tt, "history-palette-light")
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "history-palette-dark")
	tt.SetDark(false)
	tt.Frame()
	if err := tt.Click("Historical patch " + w.featureResult.Uncertain[1].Commit); err != nil {
		t.Fatal(err)
	}
	if w.source.kind != sourceHistorical || w.source.ref != w.featureResult.Uncertain[1].Commit || w.featureHistoryOpen {
		t.Fatal("conflict-only view did not open its retained changes")
	}
	if err := tt.Click("Back to feature review"); err != nil {
		t.Fatal(err)
	}
	if w.source.kind != sourceFeature || w.source.ref != branch {
		t.Fatal("home did not return to the same feature review")
	}
}

package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
	"testing"
)

func TestConflictOnlyFeatureDoesNotLookEmpty(t *testing.T) {
	w := newWindow(&git.Repo{Root: t.TempDir()}, source{kind: sourceFeature, ref: "feature"})
	w.featureBranch, w.hold = "feature", true
	w.featureResult = &review.Result{Scope: review.Scope{Branch: "feature"}, Uncertain: []review.Uncertain{{Commit: "conflict", Patch: "historical patch"}}}
	tt := ui.NewTester(w.view, 1100, 760)
	if !tt.HasText("Changes need conflict review") || tt.HasText("No local changes") {
		t.Fatal("conflict-only changes were presented as an empty feature")
	}
	snapshot(t, tt, "conflict-only-feature")
	if err := tt.Click("View changes"); err != nil {
		t.Fatal(err)
	}
	if w.source.kind != sourceHistorical || w.source.ref != "conflict" {
		t.Fatal("conflict-only view did not open its retained changes")
	}
}

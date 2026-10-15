package app

import (
	"strings"
	"testing"
)

func TestReopenedReviewRetainsFeatureAfterFastForwardAndReflogExpiry(t *testing.T) {
	w, _ := newFeatureReview(t)
	if len(w.featureRecord.Provenance) == 0 {
		t.Fatal("feature provenance was not saved with the review")
	}
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep this note after merging the feature."
	w.saveFeatureReview()
	gitIn(t, w.repo.Root, "checkout", "main")
	gitIn(t, w.repo.Root, "merge", "--ff-only", "feature")
	writeFile(t, w.repo.Root, "upstream.txt", "UNRELATED\n")
	gitIn(t, w.repo.Root, "add", "upstream.txt")
	gitIn(t, w.repo.Root, "commit", "-qm", "unrelated upstream")
	gitIn(t, w.repo.Root, "checkout", "feature")
	gitIn(t, w.repo.Root, "merge", "--ff-only", "main")
	w.load()
	if w.loadErr != nil {
		t.Fatal(w.loadErr)
	}
	gitIn(t, w.repo.Root, "reflog", "expire", "--expire=all", "--all")
	reopened := newWindow(w.repo, w.source)
	reopened.load()
	if reopened.featureResult != nil {
		defer reopened.featureResult.Close()
	}
	if reopened.loadErr != nil {
		t.Fatal(reopened.loadErr)
	}
	if !strings.Contains(reopened.featureResult.Patch, "+\treturn \"Hi, \" + name") || strings.Contains(reopened.featureResult.Patch, "upstream.txt") {
		t.Fatalf("reopening lost the feature diff or added upstream: %s", reopened.featureResult.Patch)
	}
	if len(reopened.comments) != 1 || reopened.comments[0].text != "Keep this note after merging the feature." {
		t.Fatal("reopening after integration lost the prepared review note")
	}
}

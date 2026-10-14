package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

func newFeatureReview(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "main.go", mainGo)
	var lines []string
	for i := range 300 {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	writeFile(t, dir, "long.txt", strings.Join(lines, "\n")+"\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "checkout", "-qb", "feature")
	writeFile(t, dir, "main.go", strings.Replace(mainGo, "Hello", "Hi", 1))
	lines[249] = "changed line"
	writeFile(t, dir, "long.txt", strings.Join(lines, "\n")+"\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "feature")
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(repo, source{kind: sourceFeature, ref: "feature"})
	w.load()
	if w.loadErr != nil {
		t.Fatal(w.loadErr)
	}
	t.Cleanup(func() {
		if w.featureResult != nil {
			w.featureResult.Close()
		}
	})
	return w, ui.NewTester(w.view, 1280, 860)
}

func TestReviewCommentsNavigateHiddenContext(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Handle empty names."
	w.addComment("long.txt", sideNew, 20, 20)
	w.comments[1].text = "Explain this unchanged context during review."
	w.saveFeatureReview()
	for _, f := range w.files {
		f.collapsed = true
	}
	w.filter, w.finding, w.query = "missing", true, "missing"
	tt.Frame()
	if err := tt.Click("Review comments (2)"); err != nil {
		t.Fatal(err)
	}
	first := w.orderedComments()[0]
	if !w.reviewCommentsOpen || w.reviewCommentID != first.storeID || w.filter != "" || w.finding {
		t.Fatal("comment navigation did not clear filters and select the first comment")
	}
	if !slices.ContainsFunc(w.rows, func(r row) bool { return r.kind == rowComment && r.comment == first }) || !tt.HasText("line 20") {
		t.Fatal("hidden unchanged context was not revealed")
	}
	snapshot(t, tt, "review-comments")
	if err := tt.Click("Next comment"); err != nil {
		t.Fatal(err)
	}
	if w.reviewCommentID != w.comments[0].storeID {
		t.Fatal("next comment did not navigate")
	}
	if err := tt.Click("Previous comment"); err != nil {
		t.Fatal(err)
	}
	if w.reviewCommentID != first.storeID {
		t.Fatal("previous comment did not navigate")
	}
}

func TestMissingCommentRemainsReadableAndRetainsItsAnchor(t *testing.T) {
	w, tt := newFeatureReview(t)
	saved := reviewstore.NewComment("removed.go", "removed.go", "Explain the earlier design.", reviewstore.Anchor{Side: 1, Start: 3, End: 3, Excerpt: "originalCall()"})
	w.featureRecord.Comments = []reviewstore.Comment{saved}
	w.restoreFeatureComments()
	tt.Frame()
	if err := tt.Click("Review comments (1)"); err != nil {
		t.Fatal(err)
	}
	if !w.comments[0].outdated || !tt.HasText("Location changed") {
		t.Fatal("missing file's comment was not available")
	}
	if _, ok := tt.Find("Outdated review comment"); !ok {
		t.Fatal("full outdated comment is not readable")
	}
	snapshot(t, tt, "outdated-review-comment")
	w.saveFeatureReview()
	if got := w.featureRecord.Comments[0]; got.Anchor != saved.Anchor || !got.Created.Equal(saved.Created) {
		t.Fatal("saving an outdated comment changed its original anchor or creation time")
	}
}

func TestFeatureRefreshWaitsForLatestCommentSave(t *testing.T) {
	w, _ := newFeatureReview(t)
	w.hold = true
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Unsaved background work must finish before refresh."
	w.saveFeatureReview()
	id, created := w.comments[0].storeID, w.comments[0].created
	w.loadFeature()
	// Run analysis before the queued saves to reproduce a slow disk save.
	jobs := w.held
	w.held = nil
	jobs[len(jobs)-1]()
	if w.loadErr != nil || len(w.comments) != 1 || w.comments[0].storeID != id || !w.comments[0].created.Equal(created) {
		t.Fatalf("refresh lost the latest comment: %+v, %v", w.comments, w.loadErr)
	}
	for _, job := range jobs[:len(jobs)-1] {
		job()
	}
	w.hold = false
}

package app

import (
	"fmt"
	"path/filepath"
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
	if tt.HasText("No changed files") {
		t.Fatal("comment navigation left the file tree filtered out")
	}
	snapshot(t, tt, "review-comments")
	if err := tt.Click("Comment " + w.comments[0].storeID); err != nil {
		t.Fatal(err)
	}
	if w.reviewCommentID != w.comments[0].storeID {
		t.Fatal("clicking the other comment did not navigate")
	}
	if err := tt.Click("Comment " + first.storeID); err != nil {
		t.Fatal(err)
	}
	if w.reviewCommentID != first.storeID {
		t.Fatal("clicking the first comment did not navigate")
	}
}

func TestRefreshRemovesCommentsWhoseCodeCannotBeLocated(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep this comment on code that still exists."
	w.saveFeatureReview()
	validID := w.comments[0].storeID
	db, record, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil {
		t.Fatal(err)
	}
	record.Comments = append(record.Comments,
		reviewstore.NewComment("removed.go", "removed.go", "This file no longer exists.", reviewstore.Anchor{Side: 1, Start: 3, End: 3, Excerpt: "originalCall()"}),
		reviewstore.NewComment("main.go", "main.go", "This code no longer exists.", reviewstore.Anchor{Side: 1, Start: 6, End: 6, Excerpt: "deletedCall()"}),
	)
	record.Sequence++
	if err := db.Save(record); err != nil {
		t.Fatal(err)
	}
	w.load()
	tt.Frame()
	if w.loadErr != nil || len(w.comments) != 1 || w.comments[0].storeID != validID {
		t.Fatalf("refresh did not remove only unlocatable comments: %+v, %v", w.comments, w.loadErr)
	}
	_, saved, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil || len(saved.Comments) != 1 || saved.Comments[0].ID != validID {
		t.Fatalf("cleanup was not persisted: %+v, %v", saved, err)
	}
	if err := tt.Click("Review comments (1)"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Location changed") || tt.HasText("Original code") || tt.HasText("Outdated") {
		t.Fatal("removed comments still have a separate interface")
	}
	snapshot(t, tt, "review-comments-cleaned")
	w.load()
	if w.loadErr != nil || len(w.comments) != 1 || w.comments[0].storeID != validID {
		t.Fatal("removed comments returned after reopening the review")
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

func TestClearAllReviewCommentsRequiresConfirmationAndPersists(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "A prepared review note."
	w.saveFeatureReview()
	w.addComment("long.txt", sideNew, 250, 250)
	w.comments[1].text = "Another prepared review note."
	w.setViewed(w.files[0], true)
	w.saveFeatureReview()
	otherScope := w.featureRecord.Scope
	otherScope.Branch = "another-feature"
	otherDB, otherRecord, err := reviewstore.Open(filepath.Dir(cfg.path), otherScope)
	if err != nil {
		t.Fatal(err)
	}
	otherRecord.Comments = append(otherRecord.Comments, w.featureRecord.Comments[0])
	if err := otherDB.Save(otherRecord); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Review comments (2)"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Clear all comments"); err != nil {
		t.Fatal(err)
	}
	if !w.clearReviewCommentsOpen || len(w.comments) != 2 {
		t.Fatal("opening the confirmation deleted comments")
	}
	snapshot(t, tt, "clear-review-comments")
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if len(w.comments) != 2 || w.clearReviewCommentsOpen {
		t.Fatal("canceling cleared comments")
	}
	tt.Click("Clear all comments")
	if err := tt.Click("Clear all"); err != nil {
		t.Fatal(err)
	}
	_, saved, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil || len(saved.Comments) != 0 || len(w.comments) != 0 || !w.isViewed(w.files[0]) || len(saved.Viewed) == 0 {
		t.Fatalf("clear did not persist comments only: %+v, %v", saved, err)
	}
	_, other, err := reviewstore.Open(filepath.Dir(cfg.path), otherScope)
	if err != nil || len(other.Comments) != 1 {
		t.Fatal("clearing this review changed another branch's review")
	}
	if rect, ok := tt.Find("Changes"); !ok || rect.H < 600 {
		t.Fatal("clearing comments hid the diff")
	}
}

func TestNativeReviewPreservesHeadlessDraftOwnership(t *testing.T) {
	w, _ := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Explain this in review."
	w.comments[0].storeSource = "agent:review-bot"
	w.saveFeatureReview()
	w.comments = nil
	w.restoreFeatureComments()
	if len(w.comments) != 1 || w.comments[0].storeSource != "agent:review-bot" {
		t.Fatal("restoring a CLI draft lost its owner")
	}
	w.comments[0].text = "Edited in the native review."
	w.saveFeatureReview()
	_, saved, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil || saved.Comments[0].Source != "agent:review-bot" || saved.Comments[0].Body != w.comments[0].text {
		t.Fatalf("editing a CLI draft removed its stable ownership: %+v %v", saved, err)
	}
}

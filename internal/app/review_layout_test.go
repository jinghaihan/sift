package app

import "testing"

func TestReviewSurfaceRetainsHeightWithAndWithoutCommentsPane(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Check this during CR."
	w.saveFeatureReview()
	for _, showComments := range []bool{false, true, false} {
		w.reviewCommentsOpen = showComments
		tt.Frame()
		rect, ok := tt.Find("Changes")
		if !ok || rect.H < 600 || rect.W < 300 {
			t.Fatalf("diff lost its available size (comments=%v): %+v", showComments, rect)
		}
		w.revealComment(w.comments[0])
		tt.Frame()
		if !tt.HasText(`return "Hi, " + name`) {
			t.Fatal("the actual diff code is not visible")
		}
	}
	snapshot(t, tt, "review-surface")
}

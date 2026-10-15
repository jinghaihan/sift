package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestReviewCommentHeightFollowsTextAndWindow(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	cm := w.comments[0]
	cm.text = "Keep this check before sending the request."
	w.saveFeatureReview()
	w.revealComment(cm)
	tt.Frame()
	short, ok := tt.Find("Review comment")
	if !ok || short.H >= 64 {
		t.Fatalf("short comment did not shrink below the old minimum: %+v", short)
	}
	snapshot(t, tt, "comment-auto-height-short")
	if err := tt.Click("Review comment"); err != nil {
		t.Fatal(err)
	}
	tt.Command("selectAll")
	tt.Type(strings.Repeat("Explain why this check is needed before sending the request. ", 8))
	wrapped, _ := tt.Find("Review comment")
	if wrapped.H <= short.H {
		t.Fatal("wrapped text did not increase the comment height")
	}
	tt.Command("selectAll")
	longText := strings.Repeat("Explain the validation and its effect on the request.\n", 60)
	tt.Type(longText)
	w.revealComment(cm)
	tt.Frame()
	long, _ := tt.Find("Review comment")
	if long.H <= short.H || long.H+50 > 860.0/3+1 {
		t.Fatalf("long comment did not respect the window limit: %+v", long)
	}
	tt.Key(ui.Ctrl, ui.KeyHome)
	tt.Scroll(long.X+20, long.Y+long.H/2, 0, 10000)
	if after, _ := tt.Find("Review comment"); after.Y != long.Y || after.H != long.H {
		t.Fatal("scrolling inside the comment moved the diff instead")
	}
	tt.Key(ui.Ctrl, ui.KeyEnd)
	if caret, active := tt.TextCaret(); !active || caret.Y < long.Y || caret.Y+caret.H > long.Y+long.H {
		t.Fatalf("the caret at the end of a long comment is not visible: %+v in %+v", caret, long)
	}
	tt.Key(ui.Ctrl, ui.KeyHome)
	snapshot(t, tt, "comment-auto-height-long")
	tt.SetSize(1280, 600)
	w.revealComment(cm)
	tt.Frame()
	resized, _ := tt.Find("Review comment")
	if resized.H >= long.H || resized.H+50 > 600.0/3+1 {
		t.Fatalf("resizing the window did not lower the limit: %+v", resized)
	}
	tt.SetSize(1280, 860)
	w.revealComment(cm)
	tt.Frame()
	if err := tt.Click("Review comment"); err != nil {
		t.Fatal(err)
	}
	tt.Command("selectAll")
	tt.Type("Keep this check.")
	if shrunk, _ := tt.Find("Review comment"); shrunk.H != short.H {
		t.Fatalf("shortening the text did not shrink the input: %+v", shrunk)
	}
	if w.featureRecord.Comments[0].Body != "Keep this check." {
		t.Fatal("resizing the editor affected automatic saving")
	}
}

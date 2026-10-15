package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (w *window) isFeatureReview() bool {
	return w.source.kind == sourceFeature || w.source.kind == sourceHistorical
}

func (w *window) orderedComments() []*comment {
	var list []*comment
	for _, cm := range w.comments {
		if cm.pending() {
			list = append(list, cm)
		}
	}
	slices.SortStableFunc(list, func(a, b *comment) int {
		if n := cmp.Compare(a.path, b.path); n != 0 {
			return n
		}
		if n := cmp.Compare(a.line, b.line); n != 0 {
			return n
		}
		return cmp.Compare(a.id, b.id)
	})
	return list
}

func (w *window) toggleReviewComments() {
	w.reviewCommentsOpen = !w.reviewCommentsOpen
	if !w.reviewCommentsOpen || w.loading {
		return
	}
	list := w.orderedComments()
	if len(list) == 0 {
		return
	}
	at := slices.IndexFunc(list, func(cm *comment) bool { return cm.storeID == w.reviewCommentID })
	if at < 0 {
		at = 0
	}
	w.revealComment(list[at])
}

func (w *window) revealComment(cm *comment) {
	w.reviewCommentID = cm.storeID
	list := w.orderedComments()
	if at := slices.Index(list, cm); at >= 0 {
		w.reviewCommentList.ScrollTo(at, ui.Center)
	}
	fi := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == cm.path })
	if fi < 0 {
		return
	}
	w.filter, w.finding = "", false
	if !w.fileAllowed(cm.path) {
		w.revealedExcludedPath = cm.path
		w.refreshFileFilters()
	}
	w.buildTree()
	f := w.files[fi]
	f.collapsed = false
	// Reveal just enough unchanged context for the comment, rather than
	// expanding a potentially huge file in its entirety.
	if f.canExpand() {
		for i := 0; i <= len(f.Hunks); i++ {
			gap := f.gapBefore(i)
			start := gap.newStart
			if cm.side == sideOld {
				start = gap.oldStart
			}
			if cm.line >= start && cm.line < start+gap.count {
				if f.expanded == nil {
					f.expanded = map[int]gapShown{}
				}
				shown := f.expanded[i]
				shown.top = max(shown.top, cm.line-start+4)
				f.expanded[i] = shown
			}
		}
	}
	w.rowsDirty = true
	w.buildRows()
	for i, row := range w.rows {
		if row.kind == rowComment && row.comment == cm {
			w.list.ScrollTo(i, ui.Center)
			break
		}
	}
	w.current, w.selFile, w.selHunk = fi, -1, -1
	w.revealedAt = time.Now()
	w.selectTreeFile(fi)
}

func (w *window) reviewCommentsPane(c *ui.Context, pal *palette) {
	t := c.Theme()
	list := w.orderedComments()
	ui.Column(c).Width(300).Shrink(0).Background(pal.headerBg).BorderWidth(0, 0, 0, 1).BorderColor(pal.cardBorder).Disabled(w.loading).Children(func() {
		ui.Row(c).Height(42).Padding(0, 10).Gap(5).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Text(c, "Comments").FontSize(13).Bold()
			ui.Text(c, fmt.Sprint(len(list))).FontSize(11).TextColor(t.TextMuted).Grow(1)
			w.reviewFileActions(c)
			iconButton(c, iconTrash, "Clear all comments").Disabled(len(w.comments) == 0).OnClick(func() { w.clearReviewCommentsOpen = true })
		})
		if len(list) == 0 {
			ui.Text(c, "Click a code line to write a comment. Your comments are saved automatically.").FontSize(12).TextColor(t.TextMuted).Padding(16).FillWidth()
			return
		}
		ui.List(c, &w.reviewCommentList, len(list), func(i int) {
			cm := list[i]
			c.Key(cm.storeID)
			ui.Column(c).Gap(4).FillWidth().Children(func() {
				button := ui.ButtonBase(c).FillWidth().Padding(8, 10).Radius(6).Label("Comment " + cm.storeID).Tooltip(cm.path + " · " + cm.label()).OnClick(func() { w.revealComment(cm) })
				if cm.storeID == w.reviewCommentID {
					button.Background(t.Accent.Alpha(0.1))
				} else if button.Hovered() {
					button.Background(pal.hover)
				}
				button.Children(func() {
					ui.Column(c).FillWidth().Gap(3).Children(func() {
						ui.Text(c, cm.path).FontSize(12).FontWeight(600).SingleLine().FillWidth()
						ui.Text(c, cm.label()).FontSize(11).TextColor(t.TextMuted)
						ui.Text(c, strings.TrimSpace(cm.text)).FontSize(12).MaxLines(2).FillWidth()
					})
				})
			})
		}).Grow(1).Padding(6).Label("Review comments")

	})
}

func (w *window) clearReviewCommentsDialog(c *ui.Context) {
	if !w.clearReviewCommentsOpen {
		return
	}
	if choice := ui.AlertDialog(c, &w.clearReviewCommentsOpen, "Clear all comments?", "All comments in this review will be deleted. Other reviews and viewed files will be kept.", "Cancel", "Clear all"); choice == 1 {
		w.clearReviewCommentsOpen = false
		w.comments, w.discarding = nil, nil
		w.reviewCommentID = ""
		w.rowsDirty = true
		w.saveFeatureReview()
	}
}

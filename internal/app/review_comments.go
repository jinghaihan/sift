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
		if a.outdated != b.outdated {
			if a.outdated {
				return 1
			}
			return -1
		}
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

func (w *window) stepReviewComment(direction int) {
	list := w.orderedComments()
	if len(list) == 0 {
		return
	}
	at := slices.IndexFunc(list, func(cm *comment) bool { return cm.storeID == w.reviewCommentID })
	if at < 0 {
		at = 0
	} else {
		at = max(0, min(at+direction, len(list)-1))
	}
	w.revealComment(list[at])
}

func (w *window) revealComment(cm *comment) {
	w.reviewCommentID = cm.storeID
	list := w.orderedComments()
	if at := slices.Index(list, cm); at >= 0 {
		w.reviewCommentList.ScrollTo(at, ui.Center)
	}
	if cm.outdated {
		return
	}
	fi := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == cm.path })
	if fi < 0 {
		return
	}
	w.filter, w.finding = "", false
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
	at := slices.IndexFunc(list, func(cm *comment) bool { return cm.storeID == w.reviewCommentID })
	ui.Column(c).Width(300).Shrink(0).Background(pal.headerBg).BorderWidth(0, 0, 0, 1).BorderColor(pal.cardBorder).Disabled(w.loading).Children(func() {
		ui.Row(c).Height(42).Padding(0, 10).Gap(5).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Text(c, "Comments").FontSize(13).Bold()
			ui.Text(c, fmt.Sprint(len(list))).FontSize(11).TextColor(t.TextMuted).Grow(1)
			iconButton(c, iconArrowUp, "Previous comment").Disabled(at <= 0).OnClick(func() { w.stepReviewComment(-1) })
			iconButton(c, iconArrowDown, "Next comment").Disabled(len(list) == 0 || at == len(list)-1).OnClick(func() { w.stepReviewComment(1) })
			iconButton(c, iconClose, "Close comments").OnClick(func() { w.reviewCommentsOpen = false })
		})
		if len(list) == 0 {
			ui.Text(c, "Click a code line to write a comment. Your comments are saved automatically.").FontSize(12).TextColor(t.TextMuted).Padding(16).FillWidth()
			return
		}
		ui.List(c, &w.reviewCommentList, len(list), func(i int) {
			cm := list[i]
			c.Key(cm.storeID)
			ui.Column(c).Gap(4).FillWidth().Children(func() {
				if cm.outdated && (i == 0 || !list[i-1].outdated) {
					ui.Text(c, "Location changed").FontSize(11).TextColor(t.Warning).Padding(8, 6, 4)
				}
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
		if at >= 0 && list[at].outdated {
			cm := list[at]
			ui.Column(c).Padding(12).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(pal.cardBorder).Children(func() {
				ui.Row(c).Gap(4).Children(func() {
					ui.Text(c, "Location changed").FontSize(12).TextColor(t.Warning).Grow(1)
					iconButton(c, iconClose, "Delete outdated comment").OnClick(func() { w.deleteComment(cm) })
				})
				ui.Text(c, "The original code could not be located. This comment has been kept.").FontSize(11).TextColor(t.TextMuted).FillWidth()
				ui.TextAreaBase(c, &cm.anchor.Excerpt).Height(88).Padding(8).Font(w.codeFont()).FontSize(11).ReadOnly(true).Label("Original comment context")
				area := ui.TextAreaBase(c, &cm.text).Height(112).Padding(8).FontSize(12).Label("Outdated review comment").OnChange(w.saveFeatureReview)
				if area.Focused() {
					w.typing = true
				}
			})
		}
	})
}

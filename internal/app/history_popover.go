package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
)

func (w *window) featureHistoryPopover(c *ui.Context, trigger ui.Element) {
	ui.PopoverBase(c, trigger, &w.featureHistoryOpen, func(panel ui.Element) {
		t := c.Theme()
		width, height := c.Size()
		panel.AttachTo(trigger, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(6, 0, 0, 0).
			Width(min(620, width-24)).MaxHeight(min(480, height-80)).Padding(14).Gap(8).
			Background(paletteFor(t).headerBg).Radius(10).Border(1, t.Border).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.15))
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Historical changes").FontSize(13).Bold().Grow(1)
			iconButton(c, iconClose, "Close historical changes").OnClick(func() { w.featureHistoryOpen = false })
		})
		ui.Text(c, "Historical patches, separate from the final feature changes.").FontSize(11).TextColor(t.TextMuted).FillWidth()
		search := ui.SearchField(c, &w.historicalQuery).Label("Search historical changes").FillWidth()
		if search.FocusWithin() {
			w.typing = true
		}
		var items []review.Uncertain
		query := strings.TrimSpace(w.historicalQuery)
		for _, item := range w.featureResult.Uncertain {
			if fuzzyMatch(item.Commit+" "+item.Subject+" "+item.Reason, query) {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			ui.Text(c, "No matching changes.").FontSize(12).TextColor(t.TextMuted).Padding(8)
			return
		}
		ui.List(c, &w.historicalList, len(items), func(i int) {
			item := items[i]
			c.Key(item.Commit)
			button := ui.ButtonBase(c).Padding(8).FillWidth().Radius(6).Label("Historical patch " + item.Commit).Tooltip(item.Subject).OnClick(func() {
				w.featureHistoryOpen = false
				w.setSource(source{kind: sourceHistorical, ref: item.Commit})
			})
			if button.Hovered() {
				button.Background(paletteFor(t).hover)
			}
			button.Children(func() {
				ui.Column(c).Gap(4).FillWidth().Children(func() {
					ui.Text(c, shortHash(item.Commit)+" · "+item.Subject).FontSize(12).SingleLine().FillWidth()
					ui.Text(c, item.Reason).FontSize(11).TextColor(t.Warning).SingleLine().FillWidth()
				})
			})
		}).Height(min(320, height-180, float32(len(items))*54)).FillWidth().Label("Historical patches")
	})
}

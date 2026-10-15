package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (w *window) featureHistoryPalette(c *ui.Context) {
	if !w.featureHistoryOpen || w.featureResult == nil {
		return
	}
	query := strings.TrimSpace(w.historicalQuery)
	var items []command
	for _, patch := range w.featureResult.Uncertain {
		if !fuzzyMatch(patch.Commit+" "+patch.Subject+" "+patch.Reason, query) {
			continue
		}
		kind, _, _ := strings.Cut(patch.Reason, " · ")
		items = append(items, command{
			id: patch.Commit, label: "Historical patch " + patch.Commit,
			title: patch.Subject, hint: kind, keys: shortHash(patch.Commit),
			run: func() { w.setSource(source{kind: sourceHistorical, ref: patch.Commit}) },
		})
	}
	commandPalette(c, paletteView{
		open: &w.featureHistoryOpen, query: &w.historicalQuery, row: &w.historicalRow,
		list: &w.historicalList, pointer: &w.historicalPointer,
		label: "Historical changes picker", inputLabel: "Search historical changes",
		placeholder: "Search historical changes…", icon: iconHistory, items: items,
	})
}

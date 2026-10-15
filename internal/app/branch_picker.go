package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
)

func (w *window) featureBranchPicker(c *ui.Context, pal *palette) {
	t := c.Theme()
	if w.branchQueryFor != w.featureBranch {
		w.branchQuery, w.branchQueryFor = w.featureBranch, w.featureBranch
	}
	parts := ui.ComboboxBase(c, &w.branchQuery)
	input := parts.Input.Label("Feature branch").Tooltip("Type to search branches").Width(280).MinWidth(140).Shrink(1).
		Padding(6, 10).Radius(6).Background(t.Surface).Border(1, t.Border).Disabled(w.loading)
	if input.Focused() {
		input.BorderColor(t.Accent)
		w.typing = true
	}
	if parts.Open() && !w.branchPickerOpen {
		w.refreshFeatureBranches()
	}
	w.branchPickerOpen = parts.Open()
	parts.Popup(func(panel ui.Element) {
		panel.Width(280).Radius(8).Background(pal.headerBg).Border(1, t.Border).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.15))
		ui.Scroll(c).MaxHeight(288).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(4).Children(func() {
				if w.branchRefreshErr != nil {
					ui.Text(c, "Could not refresh branches").FontSize(12).TextColor(t.Warning).Tooltip(errorText(w.branchRefreshErr)).Padding(8)
				}
				query := strings.ToLower(strings.TrimSpace(w.branchQuery))
				count := 0
				for _, branch := range w.featureBranches {
					if parts.Filtering() && !strings.Contains(strings.ToLower(branch), query) {
						continue
					}
					count++
					item := parts.Item(branch).Padding(6, 10).FillWidth().Radius(4)
					if item.Highlighted() {
						item.Background(t.Accent).TextColor(t.AccentText)
						// Keep keyboard selection visible without undoing manual scrolling
						// on every frame while the highlighted branch stays the same.
						if w.branchHighlight != branch && !item.Hovered() {
							item.ScrollIntoView()
						}
						w.branchHighlight = branch
					}
					item.Children(func() { ui.Text(c, branch).SingleLine() })
				}
				if count == 0 {
					ui.Text(c, "No matching branches").FontSize(12).TextColor(t.TextMuted).Padding(8)
				}
			})
		})
	})
	if branch, ok := parts.Chosen(); ok {
		w.branchQuery, w.branchQueryFor = branch, branch
		if branch != w.featureBranch {
			w.featureBranch = branch
			w.featureScope = review.Scope{}
			w.setSource(source{kind: sourceFeature, ref: branch})
		}
	}
	if !parts.Open() {
		w.branchQuery, w.branchHighlight = w.featureBranch, ""
	}
}

func (w *window) refreshFeatureBranches() {
	w.branchRefreshGen++
	request, gen, path := w.branchRefreshGen, w.gen, w.repo.Root
	w.background(func() {
		info, err := review.Inspect(context.Background(), path)
		w.update(func() {
			if w.branchRefreshGen != request || w.repo.Root != path || w.gen != gen {
				return
			}
			w.branchRefreshErr = err
			if err == nil {
				w.featureBranches = info.Branches
			}
		})
	})
}

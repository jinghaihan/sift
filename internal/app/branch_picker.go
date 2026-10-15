package app

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
)

func (w *window) featureBranchPicker(c *ui.Context) {
	if w.branchChoiceFor != w.featureBranch {
		w.branchChoice, w.branchChoiceFor = w.featureBranch, w.featureBranch
	}
	tip := "Type to search branches"
	if w.branchRefreshErr != nil {
		tip = "Could not refresh branches: " + errorText(w.branchRefreshErr)
	}
	picker := ui.Combobox(c, &w.branchChoice, w.featureBranches).
		Label("Feature branch").Tooltip(tip).Width(280).MinWidth(140).Shrink(1).Disabled(w.loading).
		HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
				w.branchPickerFocused = true
				w.refreshFeatureBranches()
			}
			return false
		}).OnChange(func() {
		if w.branchChoice != w.featureBranch {
			w.featureBranch, w.branchChoiceFor = w.branchChoice, w.branchChoice
			w.featureScope = review.Scope{}
			w.setSource(source{kind: sourceFeature, ref: w.featureBranch})
		}
	})
	focused := picker.FocusWithin()
	if focused {
		w.typing = true
		if !w.branchPickerFocused {
			w.refreshFeatureBranches()
		}
	}
	w.branchPickerFocused = focused
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

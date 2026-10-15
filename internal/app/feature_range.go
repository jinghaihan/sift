package app

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
)

type featureRangeState struct {
	loading      bool
	err          error
	branches     []string
	commits      []review.Commit
	integration  string
	start        string
	commitChoice string
}

func (w *window) prepareFeatureHistory() {
	state := &featureRangeState{loading: true, integration: w.featureScope.Integration, start: w.featureScope.Start}
	w.featureRange = state
	path, branch, gen := w.repo.Root, w.featureBranch, w.gen
	needsDetection := state.integration == ""
	w.background(func() {
		ctx := context.Background()
		info, err := review.Inspect(ctx, path)
		var commits []review.Commit
		if err == nil {
			commits, err = review.History(ctx, path, branch)
		}
		var detected review.Scope
		if err == nil && needsDetection {
			detected, _ = review.DetectScope(ctx, path, branch)
		}
		w.update(func() {
			if w.featureRange != state || w.repo.Root != path || w.featureBranch != branch || w.gen != gen {
				return
			}
			state.loading, state.err = false, err
			for _, name := range info.Branches {
				if name != branch {
					state.branches = append(state.branches, name)
				}
			}
			for _, commit := range commits {
				if len(commit.Parents) > 0 {
					state.commits = append(state.commits, commit)
				}
			}
			if state.integration == "" {
				state.integration = detected.Integration
			}
			if state.start == "" {
				state.start = detected.Start
			}
			if state.integration != "" && !slices.Contains(state.branches, state.integration) {
				state.branches = append(state.branches, state.integration)
			}
		})
	})
}

func (w *window) featureHistoryFallback(c *ui.Context) {
	state := w.featureRange
	if state == nil {
		return
	}
	t := c.Theme()
	ui.Column(c).Grow(1).MinHeight(0).Center().Padding(24).Children(func() {
		ui.Column(c).MaxWidth(560).FillWidth().Gap(12).Label("Feature history selection").Tooltip(errorText(w.loadErr)).Children(func() {
			if state.loading {
				ui.Spinner(c).Size(16, 16)
				return
			}
			if state.err != nil {
				ui.Text(c, errorText(state.err)).FontSize(12).TextColor(t.Warning).FillWidth()
				ui.Button(c, "Try again").OnClick(w.prepareFeatureHistory)
				return
			}
			choices := make([]string, len(state.commits))
			for i, commit := range state.commits {
				choices[i] = commit.ShortID() + " · " + commit.Subject
				if state.commitChoice == "" && state.start == commit.ID {
					state.commitChoice = choices[i]
				}
			}
			ui.Form(c, func() {
				ui.Field(c, "Integration branch", func() {
					ui.Combobox(c, &state.integration, state.branches).Label("Choose integration branch").FillWidth()
				})
				ui.Field(c, "First feature commit", func() {
					ui.Combobox(c, &state.commitChoice, choices).Label("Choose first feature commit").FillWidth().OnChange(func() {
						if i := slices.Index(choices, state.commitChoice); i >= 0 {
							state.start = state.commits[i].ID
						}
					})
				})
			}).FillWidth()
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				ui.PrimaryButton(c, "Review feature").Disabled(state.start == "" || state.integration == "").OnClick(func() {
					w.featureScope = review.Scope{Repository: w.repo.Root, Branch: w.featureBranch, Integration: state.integration, Start: state.start}
					if target := (source{kind: sourceFeature, ref: w.featureBranch}); w.source != target {
						w.setSource(target)
					} else {
						w.load()
					}
				})
			})
		})
	})
}

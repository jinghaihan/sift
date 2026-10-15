package app

import (
	"context"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
)

type featureRangeState struct {
	loading     bool
	err         error
	branches    []string
	commits     []review.Commit
	integration string
	start       string
	query       string
	list        ui.ListState
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
	pal := paletteFor(t)
	width, height := c.Size()
	ui.Column(c).Grow(1).MinHeight(0).Center().Children(func() {
		ui.Column(c).Width(min(620, width-48)).MaxHeight(height - 80).Label("Feature history selection").Children(func() {
			ui.Column(c).Padding(14, 14, 8).Gap(10).FillWidth().Children(func() {
				ui.Text(c, "Choose feature history").FontSize(15).Bold()
				ui.Text(c, errorText(w.loadErr)).FontSize(12).TextColor(t.TextMuted).FillWidth()
				ui.Text(c, w.featureBranch).FontSize(12).TextColor(t.TextMuted).SingleLine().FillWidth()
				if state.loading {
					ui.Spinner(c).Size(16, 16)
					return
				}
				if state.err != nil {
					ui.Text(c, errorText(state.err)).FontSize(12).TextColor(t.Warning).FillWidth()
					ui.Button(c, "Try again").OnClick(w.prepareFeatureHistory)
					return
				}
				ui.Text(c, "Integration branch").FontSize(12).FontWeight(600).Tooltip("The branch you merge into this feature")
				ui.Combobox(c, &state.integration, state.branches).Label("Choose integration branch").FillWidth()
				ui.Text(c, "First feature commit (included)").FontSize(12).FontWeight(600)
				ui.SearchField(c, &state.query).Label("Search feature commits").Placeholder("Search commits").FillWidth().OnChange(func() { state.list.ScrollTo(0, ui.Start) })
			})
			if state.loading || state.err != nil {
				return
			}
			var matches []review.Commit
			for _, commit := range state.commits {
				if fuzzyMatch(commit.ID+" "+commit.Subject+" "+commit.Date, strings.TrimSpace(state.query)) {
					matches = append(matches, commit)
				}
			}
			if len(matches) == 0 {
				ui.Text(c, "No matching commits").FontSize(12).TextColor(t.TextMuted).Padding(12, 14)
			} else {
				ui.List(c, &state.list, len(matches), func(i int) {
					commit := matches[i]
					c.Key(commit.ID)
					button := ui.ButtonBase(c).Padding(8).Radius(6).FillWidth().Label("Start at " + commit.ID).Tooltip(commit.Subject).
						OnClick(func() { state.start = commit.ID })
					if state.start == commit.ID || button.Hovered() {
						button.Background(pal.hover)
					}
					button.Children(func() {
						ui.Column(c).Gap(4).FillWidth().Children(func() {
							ui.Row(c).Gap(6).Children(func() {
								ui.Text(c, commit.Subject).FontSize(12).SingleLine().Grow(1).MinWidth(0)
								if state.start == commit.ID {
									ui.Icon(c, iconCheck).FontSize(13)
								}
							})
							ui.Text(c, commit.ShortID()+" · "+commit.Date).FontSize(11).TextColor(t.TextMuted)
						})
					})
				}).Height(min(280, height-310)).Padding(0, 14).FillWidth().Label("Feature commit choices")
			}
			ui.Column(c).Padding(12, 14, 14).Gap(10).FillWidth().Children(func() {
				if state.start != "" {
					ui.Text(c, shortHash(state.start)+" → "+w.featureBranch+" (latest)").FontSize(11).TextColor(t.TextMuted).SingleLine().FillWidth().Label("Selected review range")
				}
				ui.Row(c).Gap(8).Justify(ui.End).FillWidth().Children(func() {
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
	})
}

package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

type exclusionDraft struct {
	id int
	fileExclusion
}

func (w *window) addExclusionDraft(rule fileExclusion) {
	w.exclusionNextID++
	w.exclusionDrafts = append(w.exclusionDrafts, &exclusionDraft{id: w.exclusionNextID, fileExclusion: rule})
}

func (w *window) toggleFileExclusions() {
	w.exclusionsOpen = !w.exclusionsOpen
	if !w.exclusionsOpen {
		return
	}
	w.exclusionDrafts = nil
	for _, rule := range w.settings.FileExclusions {
		w.addExclusionDraft(rule)
	}
}

func (w *window) syncFileExclusions() {
	rules := make([]fileExclusion, 0, len(w.exclusionDrafts))
	for _, draft := range w.exclusionDrafts {
		rule := draft.fileExclusion
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		if _, err := compilePathGlob(rule.Pattern); err != nil {
			continue
		}
		rules = append(rules, rule)
	}
	s := w.settings
	s.FileExclusions = rules
	w.applySettings(s)
	w.revealedExcludedPath = ""
	w.fileFiltersDirty = true
	cfg.SetFileExclusions(rules)
}

func (w *window) fileExclusionsButton(c *ui.Context, pal *palette) {
	t := c.Theme()
	hidden := w.hiddenFileCount()
	active := len(w.exclusionMatchers) > 0
	trigger := iconButton(c, iconFilter, "File exclusions").Tooltip(fmt.Sprintf("File exclusions · %d files hidden", hidden)).OnClick(w.toggleFileExclusions)
	if active {
		trigger.Background(pal.hover).TextColor(t.Text)
	}
	ui.PopoverBase(c, trigger, &w.exclusionsOpen, func(panel ui.Element) {
		width, height := c.Size()
		panel.AttachTo(trigger, ui.AnchorBottomLeft, ui.AnchorTopLeft).Margin(6, 0, 0, 0).
			Width(min(390, width-24)).MaxHeight(height-24).Padding(14).Gap(10).Background(pal.headerBg).
			Radius(10).Border(1, t.Border).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.15))
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, "File exclusions").FontSize(13).Bold().Grow(1)
			iconButton(c, iconClose, "Close file exclusions").OnClick(func() { w.exclusionsOpen = false })
		})
		ui.Scroll(c).MaxHeight(min(280, height-260)).FillWidth().Children(func() {
			ui.Column(c).Gap(6).FillWidth().Children(func() {
				for i, draft := range w.exclusionDrafts {
					c.Key(draft.id)
					ui.Column(c).Gap(3).FillWidth().Children(func() {
						ui.Row(c).Gap(6).FillWidth().Children(func() {
							ui.Checkbox(c, &draft.Enabled, "").Label(fmt.Sprintf("Enable rule %d", i+1)).Tooltip("Enable this rule").OnChange(w.syncFileExclusions)
							input := ui.TextInput(c, &draft.Pattern).Label(fmt.Sprintf("Exclude pattern %d", i+1)).Placeholder("*.md").Grow(1).MinWidth(0).OnChange(w.syncFileExclusions)
							if input.Focused() {
								w.typing = true
							}
							iconButton(c, iconTrash, fmt.Sprintf("Delete rule %d", i+1)).OnClick(func() {
								w.exclusionDrafts = slices.DeleteFunc(w.exclusionDrafts, func(other *exclusionDraft) bool { return other == draft })
								w.syncFileExclusions()
							})
						})
						if _, err := compilePathGlob(draft.Pattern); err != nil && draft.Pattern != "" {
							ui.Text(c, err.Error()).FontSize(11).TextColor(t.Warning).FillWidth()
						}
					})
				}
				if len(w.exclusionDrafts) == 0 {
					ui.Text(c, "No rules").FontSize(12).TextColor(t.TextMuted).Padding(6).FillWidth()
				}
			})
		})
		ui.Button(c, "Add rule").Label("Add exclusion rule").FillWidth().Margin(8, 0, 0).OnClick(func() { w.addExclusionDraft(fileExclusion{Enabled: true}) })
	})
}

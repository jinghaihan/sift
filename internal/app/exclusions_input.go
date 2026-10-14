package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func formatFileExclusions(rules []fileExclusion) string {
	var patterns []string
	for _, rule := range rules {
		if rule.Enabled {
			patterns = append(patterns, rule.Pattern)
		}
	}
	return strings.Join(patterns, ", ")
}

// Commas inside character classes or escaped with a backslash are literal.
func parseFileExclusions(text string) ([]fileExclusion, string) {
	rules := make([]fileExclusion, 0)
	invalid := ""
	add := func(pattern string) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return
		}
		if _, err := compilePathGlob(pattern); err != nil {
			invalid = fmt.Sprintf("Invalid pattern: %s", pattern)
			return
		}
		rules = append(rules, fileExclusion{Pattern: pattern, Enabled: true})
	}
	start, inClass, escaped := 0, false, false
	for i, ch := range text {
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '[':
			inClass = true
		case ']':
			inClass = false
		case ',':
			if !inClass {
				add(text[start:i])
				start = i + 1
			}
		}
	}
	add(text[start:])
	return rules, invalid
}

func (w *window) syncFileExclusions() {
	rules, invalid := parseFileExclusions(w.exclusionsText)
	// Keep the exact edit (including spaces and unfinished patterns) in the field.
	w.exclusionsError = invalid
	w.settings.FileExclusions = rules
	w.compileFileExclusions()
	w.revealedExcludedPath = ""
	w.fileFiltersDirty = true
	cfg.SetFileExclusions(rules)
}

func (w *window) toggleFileExclusions() {
	w.exclusionsOpen = !w.exclusionsOpen
	w.exclusionsFocus = w.exclusionsOpen
}

func (w *window) fileExclusionsButton(c *ui.Context, pal *palette) {
	trigger := iconButton(c, iconFilter, "File exclusions").
		Tooltip(fmt.Sprintf("File exclusions · %d files hidden", w.hiddenFileCount())).
		OnClick(w.toggleFileExclusions)
	if len(w.exclusionMatchers) > 0 || w.exclusionsOpen {
		trigger.Background(pal.hover).TextColor(c.Theme().Text)
	}
}

func (w *window) fileExclusionsInput(c *ui.Context) {
	t := c.Theme()
	c.Key("file-exclusions-input")
	box := ui.Row(c).Height(28).FillWidth().Margin(6, 0, 0).Padding(0, 6, 0, 8).Gap(6).
		Radius(7).Background(ui.RGBA(127, 127, 127, 0.12))
	box.Children(func() {
		input := ui.TextInputBase(c, &w.exclusionsText).Label("Files to exclude").
			Placeholder("Files to exclude").Tooltip("Files to exclude: *.md, *.map").
			FontSize(13).Grow(1).MinWidth(0).OnChange(w.syncFileExclusions)
		if w.exclusionsFocus {
			input.Focus()
			w.exclusionsFocus = false
		}
		if input.Focused() {
			w.typing = true
			box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
		}
		if w.exclusionsError != "" {
			box.Border(1, t.Warning).Tooltip(w.exclusionsError)
		}
		if w.exclusionsText != "" {
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().
				Background(t.TextMuted.Alpha(0.45)).Label("Clear exclusions").Tooltip("Clear exclusions").FocusRing(false)
			clear.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.Background) })
			clear.OnClick(func() { w.exclusionsText = ""; w.syncFileExclusions() })
		}
	})
}

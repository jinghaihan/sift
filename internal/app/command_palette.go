package app

import "github.com/egoist/mygo/ui"

// paletteView binds a picker to persistent window data. The command bar and
// historical changes share the same rendering and keyboard interaction.
type paletteView struct {
	open                           *bool
	query                          *string
	row                            *int
	list                           *ui.ListState
	pointer                        *[2]float32
	label, inputLabel, placeholder string
	icon                           *ui.SVG
	items                          []command
}

func commandPalette(c *ui.Context, state paletteView) {
	t := c.Theme()
	pal := paletteFor(t)
	if *state.row >= len(state.items) {
		*state.row = len(state.items) - 1
	}
	if *state.row < 0 && len(state.items) > 0 {
		*state.row = 0
	}
	run := func(i int) {
		if i < 0 || i >= len(state.items) {
			return
		}
		*state.open, *state.query = false, ""
		state.items[i].run()
	}
	c.Key(state.label)
	ui.DialogBase(c, state.open, func(backdrop, panel ui.Element) {
		width, height := c.Size()
		top := min(float32(120), max(float32(16), height/5))
		backdrop.Background(ui.RGBA(0, 0, 0, 0.12)).Justify(ui.Start).Padding(top, 0, 0, 0)
		panel.Width(min(560, width-32)).MaxHeight(min(440, height-top-16)).Radius(16).
			Background(pal.headerBg).Border(1, pal.cardBorder).
			Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Label(state.label)
		ui.Row(c).Padding(10, 14).Gap(10).BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Children(func() {
			ui.Icon(c, state.icon).FontSize(16).TextColor(t.TextMuted)
			input := ui.TextInputBase(c, state.query).Placeholder(state.placeholder).Label(state.inputLabel).FontSize(15).Grow(1).AutoFocus()
			if input.Changed() {
				*state.row = 0
				state.list.ScrollIntoView(0)
			}
			if input.Shortcut(0, ui.KeyDown) && len(state.items) > 0 {
				*state.row = (*state.row + 1) % len(state.items)
				state.list.ScrollIntoView(*state.row)
			}
			if input.Shortcut(0, ui.KeyUp) && len(state.items) > 0 {
				*state.row = (*state.row - 1 + len(state.items)) % len(state.items)
				state.list.ScrollIntoView(*state.row)
			}
			if input.Submitted() {
				run(*state.row)
			}
		})
		state.list.Key = func(i int) any {
			if state.items[i].id != "" {
				return state.items[i].id
			}
			return state.items[i].title
		}
		ui.List(c, state.list, len(state.items), func(i int) {
			cmd := state.items[i]
			label := cmd.label
			if label == "" {
				label = cmd.title
			}
			item := ui.Row(c).MinHeight(34).Padding(6, 10).Margin(0, 6).Gap(8).Radius(8).
				Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label).Tooltip(cmd.title)
			if item.Hovered() && palettePointerMoved(item, state.pointer) {
				*state.row = i
			}
			if i == *state.row {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			if item.Clicked() {
				run(i)
			}
			item.Children(func() {
				ui.Text(c, cmd.title).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
				if cmd.hint != "" {
					hint := ui.Text(c, cmd.hint).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
					if i != *state.row {
						hint.TextColor(t.TextMuted)
					} else {
						hint.Opacity(0.8)
					}
				}
				ui.Spacer(c)
				if cmd.keys != "" {
					key := ui.Text(c, cmd.keys).FontSize(11).Padding(2, 6).Radius(5).Shrink(0)
					if i == *state.row {
						key.Background(ui.RGBA(255, 255, 255, 0.2))
					} else {
						key.Background(pal.pill).TextColor(t.TextMuted)
					}
				}
			})
		}).Padding(6, 0).MaxHeight(min(380, height-top-60)).FillWidth().Label(state.label + " choices").Children(func() {
			if len(state.items) == 0 {
				ui.Text(c, "No matching results").FontSize(13).TextColor(t.TextMuted).Padding(12)
			}
		})
	})
	if !*state.open {
		*state.query = ""
	}
}

func palettePointerMoved(e ui.Element, pointer *[2]float32) bool {
	x, y, over := e.PointerPosition()
	if !over {
		return false
	}
	moved := x != pointer[0] || y != pointer[1]
	*pointer = [2]float32{x, y}
	return moved
}

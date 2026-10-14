package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/diff"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

func (w *window) loadFeature() {
	if w.featureCancel != nil {
		w.featureCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.featureCancel = cancel
	w.gen++
	gen, src := w.gen, w.source
	w.genA.Store(int64(gen))
	w.loading, w.loadErr, w.loadStatus = true, nil, "Isolating complete feature changes…"
	w.loadStart = time.Now()
	directory := filepath.Dir(cfg.path)
	requested := w.featureScope
	branch := src.ref
	if src.kind == sourceHistorical {
		branch = w.featureBranch
	}
	previous := w.featureResult
	w.background(func() {
		var result *review.Result
		var patches []*diff.File
		var content map[string]loaded
		var record reviewstore.Record
		var database *reviewstore.Database
		var branches []string
		var err error
		info, err := review.Inspect(ctx, w.repo.Root)
		branches = info.Branches
		if err == nil && src.kind == sourceFeature {
			scope := requested
			if scope.Repository != w.repo.Root || scope.Branch != branch || scope.Start == "" {
				saved, loadErr := reviewstore.LoadSettings(directory)
				if loadErr != nil {
					err = loadErr
				} else if saved.Repository == w.repo.Root && saved.Branch == branch && saved.Start != "" {
					scope = saved
				} else {
					scope, err = review.DetectScope(ctx, w.repo.Root, branch)
				}
			}
			if err == nil {
				var value review.Result
				value, err = review.Analyze(ctx, review.Options{Scope: scope, KeepSnapshot: true, OnProgress: func(message string) {
					w.update(func() {
						if !w.stale(gen) {
							w.loadStatus = message
						}
					})
				}})
				if err == nil {
					result = &value
					patches = diff.Parse([]byte(value.Patch))
					content, err = w.featureContents(ctx, result, patches)
				}
			}
		} else if err == nil {
			result = previous
			if result == nil {
				err = fmt.Errorf("load the feature before its historical changes")
			} else {
				for _, historical := range result.Uncertain {
					if historical.Commit == src.ref {
						patches = diff.Parse([]byte(historical.Patch))
						break
					}
				}
				if patches == nil {
					err = fmt.Errorf("historical patch not found")
				}
			}
		}
		if err == nil {
			scope := result.Scope
			if src.kind == sourceHistorical {
				scope.Branch += "::historical:" + src.ref
			}
			database, record, err = reviewstore.Open(directory, scope)
			if err == nil {
				record.Tip = result.Tip
				if src.kind == sourceFeature {
					err = reviewstore.SaveSettings(directory, result.Scope)
				}
			}
		}
		if ctx.Err() != nil || w.stale(gen) {
			if result != nil && result != previous {
				_ = result.Close()
			}
			return
		}
		w.update(func() {
			if w.stale(gen) {
				if result != nil && result != previous {
					go result.Close()
				}
				return
			}
			w.loading, w.loadErr, w.loadStatus = false, err, ""
			w.featureBranches, w.featureBranch = branches, branch
			if err != nil {
				if result != nil && result != previous {
					go result.Close()
				}
				return
			}
			if result != previous {
				if previous != nil {
					go previous.Close()
				}
				w.featureResult = result
			}
			w.featureScope = result.Scope
			w.featureDB, w.featureRecord = database, record
			w.comments = nil
			w.viewed = maps.Clone(record.Viewed)
			for _, f := range patches {
				f.Generated = git.LooksGenerated(f.Path)
			}
			w.setFiles(patches)
			for _, f := range w.files {
				if value, ok := content[f.Path]; ok {
					value.file = f
					value.apply()
				} else {
					f.loaded = true
				}
			}
			w.restoreFeatureComments()
			w.loadedOnce, w.rowsDirty = true, true
			w.buildTree()
		})
	})
}

func (w *window) featureContents(ctx context.Context, result *review.Result, files []*diff.File) (map[string]loaded, error) {
	snapshot, err := git.Open(result.SnapshotPath)
	if err != nil {
		return nil, err
	}
	reader, err := snapshot.NewContents()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	values := map[string]loaded{}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value := loaded{path: f.Path, oldPath: f.OldPath}
		// Generated artifacts still have their exact patch. Avoid spending the
		// initial review's highlighting budget on multi-megabyte bundled code.
		if !git.LooksGenerated(f.Path) {
			value = w.read(reader, f, result.Baseline, result.Tip)
			value.finish()
		}
		values[f.Path] = value
	}
	return values, nil
}

func (w *window) restoreFeatureComments() {
	byPath := map[string]*fileState{}
	for _, f := range w.files {
		byPath[f.Path] = f
	}
	for _, saved := range w.featureRecord.Comments {
		anchor := saved.Anchor
		outdated := true
		if f := byPath[saved.Path]; f != nil {
			lines := map[int]string{}
			text := f.newLines
			if anchor.Side == 0 {
				text = f.oldLines
			}
			for i, line := range text {
				lines[i+1] = line
			}
			// Historical and generated patches have only their numbered context.
			if text == nil {
				for _, h := range f.Hunks {
					for _, line := range h.Lines {
						n := line.New
						if anchor.Side == 0 {
							n = line.Old
						}
						if n > 0 {
							lines[n] = line.Text
						}
					}
				}
			}
			anchor, outdated = reviewstore.Relocate(anchor, lines)
			outdated = !outdated
		}
		commentIDs++
		sd := sideNew
		if anchor.Side == 0 {
			sd = sideOld
		}
		w.comments = append(w.comments, &comment{id: commentIDs, path: saved.Path, side: sd, start: anchor.Start, line: anchor.End, text: saved.Body, storeID: saved.ID, outdated: outdated})
	}
}

func (w *window) featureRecordSnapshot() reviewstore.Record {
	record := w.featureRecord.Clone()
	record.Viewed = maps.Clone(w.viewed)
	record.Comments = nil
	for _, cm := range w.comments {
		if !cm.pending() {
			continue
		}
		anchor := reviewstore.Anchor{Side: 1, Start: cm.start, End: cm.line}
		if anchor.Start == 0 {
			anchor.Start = anchor.End
		}
		if cm.side == sideOld {
			anchor.Side = 0
		}
		var lines []string
		for _, f := range w.files {
			if f.Path != cm.path {
				continue
			}
			text := f.newLines
			if cm.side == sideOld {
				text = f.oldLines
			}
			numbered := map[int]string{}
			for i, line := range text {
				numbered[i+1] = line
			}
			if text == nil {
				for _, h := range f.Hunks {
					for _, line := range h.Lines {
						n := line.New
						if cm.side == sideOld {
							n = line.Old
						}
						if n > 0 {
							numbered[n] = line.Text
						}
					}
				}
			}
			for n := anchor.Start; n <= anchor.End; n++ {
				lines = append(lines, numbered[n])
			}
			anchor.Before = numbered[anchor.Start-1]
			anchor.After = numbered[anchor.End+1]
			break
		}
		anchor.Excerpt = strings.Join(lines, "\n")
		if cm.outdated {
			for _, saved := range w.featureRecord.Comments {
				if saved.ID == cm.storeID {
					anchor = saved.Anchor
					break
				}
			}
		}
		value := reviewstore.NewComment(cm.path, cm.path, cm.text, anchor)
		if cm.storeID == "" {
			cm.storeID = value.ID
		} else {
			value.ID = cm.storeID
		}
		value.Outdated = cm.outdated
		record.Comments = append(record.Comments, value)
	}
	return record
}

func (w *window) saveFeatureReview() {
	if w.featureDB == nil || w.source.kind != sourceFeature && w.source.kind != sourceHistorical {
		return
	}
	expectedBranch := w.source.ref
	if w.source.kind == sourceHistorical {
		expectedBranch = w.featureBranch + "::historical:" + w.source.ref
	}
	if w.featureRecord.Scope.Branch != expectedBranch {
		return
	}
	w.featureRecord.Sequence++
	record, db := w.featureRecordSnapshot(), w.featureDB
	w.featureRecord = record
	w.background(func() {
		if err := db.Save(record); err != nil {
			w.update(func() {
				if w.featureDB == db {
					w.loadErr = fmt.Errorf("save review: %w", err)
				}
			})
		}
	})
}

func (w *window) featureDialogs(c *ui.Context) {
	if w.featureRangeOpen {
		ui.DialogBase(c, &w.featureRangeOpen, func(backdrop, panel ui.Element) {
			panel.Width(560).Padding(20).Gap(14).Background(paletteFor(c.Theme()).headerBg).Radius(12).Border(1, c.Theme().Border)
			ui.Text(c, "Review range").FontSize(18).Bold()
			ui.Text(c, "The range could not be inferred reliably. Set it once for this branch.").TextColor(c.Theme().TextMuted).FillWidth()
			ui.Field(c, "Integration branch", func() { ui.TextInput(c, &w.featureRangeIntegration).Label("Integration branch").FillWidth() })
			ui.Field(c, "First feature commit (inclusive)", func() { ui.TextInput(c, &w.featureRangeStart).Label("First feature commit").FillWidth() })
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "Cancel").OnClick(func() { w.featureRangeOpen = false })
				ui.PrimaryButton(c, "Review").Disabled(w.featureRangeStart == "" || w.featureRangeIntegration == "").OnClick(func() {
					w.featureScope = review.Scope{Repository: w.repo.Root, Branch: w.featureBranch, Integration: w.featureRangeIntegration, Start: w.featureRangeStart}
					w.featureRangeOpen = false
					w.load()
				})
			})
		})
	}
	if w.featureHistoryOpen && w.featureResult != nil {
		ui.DialogBase(c, &w.featureHistoryOpen, func(backdrop, panel ui.Element) {
			panel.Width(700).MaxHeight(560).Padding(18).Gap(6).Background(paletteFor(c.Theme()).headerBg).Radius(12).Border(1, c.Theme().Border)
			ui.Row(c).Gap(10).Children(func() {
				ui.Text(c, "Historical changes").FontSize(18).Bold().Grow(1)
				iconButton(c, iconClose, "Close").OnClick(func() { w.featureHistoryOpen = false })
			})
			ui.Text(c, "These conflict patches are historical comparisons, separate from the net feature diff.").FillWidth().TextColor(c.Theme().TextMuted)
			ui.Scroll(c).MaxHeight(420).FillWidth().Children(func() {
				ui.Column(c).Gap(4).FillWidth().Children(func() {
					for _, item := range w.featureResult.Uncertain {
						c.Key(item.Commit)
						ui.ButtonBase(c).Padding(8).FillWidth().Label(item.Commit[:9]).OnClick(func() { w.featureHistoryOpen = false; w.setSource(source{kind: sourceHistorical, ref: item.Commit}) }).Children(func() {
							ui.Column(c).Gap(4).FillWidth().Children(func() {
								ui.Text(c, item.Commit[:9]+" · "+item.Subject).FontSize(12).SingleLine()
								ui.Text(c, item.Reason).FontSize(11).TextColor(c.Theme().Warning)
							})
						})
					}
				})
			})
		})
	}
}

func (w *window) exportFeatureReview() {
	text := w.commentsMarkdown()
	if text == "" {
		return
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w.win, Title: "Export review", DefaultPath: "sift-review.md"})
		if err == nil && path != "" {
			err = os.WriteFile(path, []byte(text), 0o600)
		}
		if err != nil {
			w.update(func() { w.loadErr = err })
		}
	}()
}

func (w *window) featureToolbar(c *ui.Context, pal *palette) {
	t := c.Theme()
	left, right := w.toolbarInsets(c)
	ui.Row(c).Height(titleBarHeight).Padding(0, right, 0, left).Gap(10).AlignItems(ui.Center).DragWindow().BorderWidth(0, 0, 1, 0).BorderColor(pal.cardBorder).Background(pal.headerBg.Alpha(0.6)).Children(func() {
		if !w.sidebarShown {
			w.sidebarToggle(c)
		}
		repo := ui.ButtonBase(c).Height(30).Padding(0, 6).Gap(6).Radius(6).Tooltip("Choose repository · " + w.repo.Root).Label("Open repository").OnClick(w.chooseRepository)
		if repo.Hovered() {
			repo.Background(pal.hover)
		}
		repo.Children(func() {
			ui.Icon(c, iconFolder).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, filepath.Base(w.repo.Root)).FontSize(13).FontWeight(600).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Select(c, &w.featureBranch, w.featureBranches).Label("Feature branch").MaxWidth(280).Disabled(w.loading).OnChange(func() {
			w.featureScope = review.Scope{}
			w.setSource(source{kind: sourceFeature, ref: w.featureBranch})
		})
		if w.source.kind == sourceHistorical {
			iconButton(c, iconHouse, "Back to feature review").OnClick(func() { w.setSource(source{kind: sourceFeature, ref: w.featureBranch}) })
			ui.Text(c, "Historical").FontSize(11).TextColor(t.Warning)
		}
		if w.loading {
			ui.Spinner(c).Size(14, 14).Tooltip(w.loadStatus)
		}
		ui.Spacer(c)
		if result := w.featureResult; result != nil && result.Branch == w.featureBranch && len(result.Uncertain) > 0 {
			iconButton(c, iconAlert, fmt.Sprintf("%d historical conflict patches", len(result.Uncertain))).TextColor(t.Warning).OnClick(func() { w.featureHistoryOpen = true })
		}
		if w.loadErr != nil {
			iconButton(c, iconBranch, "Set review range").OnClick(func() {
				w.featureRangeStart, w.featureRangeIntegration = w.featureScope.Start, w.featureScope.Integration
				w.featureRangeOpen = true
			})
		}
		iconButton(c, iconSearch, "Find in diffs (⌘F)").OnClick(func() { w.finding = !w.finding })
		wrap := iconButton(c, iconWrap, "Toggle word wrap (⌥Z)").OnClick(func() { w.settings.WordWrap = !w.settings.WordWrap; w.rowsDirty = true })
		if w.settings.WordWrap {
			wrap.Background(pal.hover).TextColor(t.Text)
		}
		iconButton(c, iconComment, "Copy review comments ("+strconv.Itoa(w.pendingComments())+")").Disabled(w.pendingComments() == 0).OnClick(w.copyComments)
		iconButton(c, iconOpen, "Export review…").Disabled(w.pendingComments() == 0).OnClick(w.exportFeatureReview)
		if w.loading {
			iconButton(c, iconStop, "Cancel analysis").OnClick(func() {
				if w.featureCancel != nil {
					w.featureCancel()
				}
				w.gen++
				w.genA.Store(int64(w.gen))
				w.loading = false
			})
		} else {
			iconButton(c, iconRefresh, "Refresh feature diff (⌘R)").OnClick(w.refresh)
		}
		w.layoutControl(c, pal)
		themeIcon, themeTip, nextTheme := iconMoon, "Switch to dark theme", "dark"
		if t.Dark {
			themeIcon, themeTip, nextTheme = iconSun, "Switch to light theme", "light"
		}
		iconButton(c, themeIcon, themeTip).OnClick(func() {
			go cfg.Update(func(settings *Settings) { settings.Theme = nextTheme })
		})
	})
}

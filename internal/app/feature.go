package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/diff"
	"github.com/jinghaihan/sift/internal/git"
	"github.com/jinghaihan/sift/internal/review"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

func (w *window) loadFeature() {
	w.featureRange = nil
	w.featureNeedsScope = false
	w.featureHistoryOpen = false
	w.clearReviewCommentsOpen = false
	w.saveFeatureReview()
	pendingDB, pendingRecord := w.featureDB, w.featureRecord.Clone()
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
		var needsScope bool
		var err error
		// Finish the latest save before reopening its record on refresh or a
		// quick source switch. Older queued saves are rejected by sequence.
		if pendingDB != nil {
			err = pendingDB.Save(pendingRecord)
		}
		var info review.Repository
		if err == nil {
			info, err = review.Inspect(ctx, w.repo.Root)
		}
		branches = info.Branches
		if err == nil && src.kind == sourceFeature {
			scope := requested
			if scope.Repository != w.repo.Root || scope.Branch != branch || scope.Start == "" {
				saved, loadErr := reviewstore.LoadScope(directory, w.repo.Root, branch)
				if loadErr != nil {
					err = loadErr
				} else if saved.Repository == w.repo.Root && saved.Branch == branch && saved.Start != "" {
					scope = saved
				} else {
					scope, err = review.DetectScope(ctx, w.repo.Root, branch)
					needsScope = err != nil && !errors.Is(err, review.ErrNoFeatureChanges)
				}
			}
			if err == nil {
				database, record, err = reviewstore.Open(directory, scope)
			}
			if err == nil {
				var value review.Result
				value, err = review.Analyze(ctx, review.Options{Scope: scope, KnownCommits: record.Provenance, KeepSnapshot: true, OnProgress: func(message string) {
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
			if database == nil || record.Scope != scope {
				database, record, err = reviewstore.Open(directory, scope)
			}
			if err == nil {
				record.Tip = result.Tip
				if src.kind == sourceFeature {
					record.Provenance = result.Provenance
					err = reviewstore.SaveScope(directory, result.Scope)
					if err == nil {
						record.Sequence++
						err = database.Save(record)
					}
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
			w.featureNeedsScope = needsScope || errors.Is(err, review.ErrInvalidScope)
			w.featureBranches, w.featureBranch = branches, branch
			if err != nil {
				if src.kind == sourceFeature && (w.featureNeedsScope || errors.Is(err, review.ErrNoFeatureChanges)) {
					w.featureDB, w.featureRecord = nil, reviewstore.Record{}
					w.comments, w.viewed = nil, nil
					w.setFiles(nil)
					w.buildTree()
					w.featureResult = nil
					if previous != nil {
						go previous.Close()
					}
				}
				if w.featureNeedsScope && src.kind == sourceFeature {
					w.prepareFeatureHistory()
				}
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
			if w.restoreFeatureComments() > 0 {
				w.saveFeatureReview()
			}
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

// restoreFeatureComments relocates saved notes and counts notes whose code
// no longer exists so the caller can persist their removal.
func (w *window) restoreFeatureComments() int {
	restored := 0
	byPath := map[string]*fileState{}
	for _, f := range w.files {
		byPath[f.Path] = f
	}
	for _, saved := range w.featureRecord.Comments {
		anchor := saved.Anchor
		located := false
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
			anchor, located = reviewstore.Relocate(anchor, lines)
		}
		if !located {
			continue
		}
		restored++
		commentIDs++
		sd := sideNew
		if anchor.Side == 0 {
			sd = sideOld
		}
		w.comments = append(w.comments, &comment{id: commentIDs, path: saved.Path, side: sd, start: anchor.Start, line: anchor.End, text: saved.Body, storeID: saved.ID, storeSource: saved.Source, created: saved.Created, resolved: saved.Resolved})
	}
	return len(w.featureRecord.Comments) - restored
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
		anchor.Start, anchor.End = min(anchor.Start, anchor.End), max(anchor.Start, anchor.End)
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
		value := reviewstore.NewComment(cm.path, cm.path, cm.text, anchor)
		if cm.storeID == "" {
			cm.storeID = value.ID
		} else {
			value.ID = cm.storeID
		}
		if cm.created.IsZero() {
			cm.created = value.Created.UTC()
		}
		value.Created, value.Resolved = cm.created, cm.resolved
		value.Source = cm.storeSource
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
		w.featureBranchPicker(c)
		if w.source.kind == sourceHistorical {
			iconButton(c, iconHouse, "Back to feature review").OnClick(func() { w.setSource(source{kind: sourceFeature, ref: w.featureBranch}) })
			ui.Text(c, "Historical").FontSize(11).TextColor(t.Warning)
		}
		if w.loading {
			ui.Spinner(c).Size(14, 14).Tooltip(w.loadStatus)
		}
		ui.Spacer(c)
		if result := w.featureResult; result != nil && result.Branch == w.featureBranch && len(result.Uncertain) > 0 {
			iconButton(c, iconAlert, fmt.Sprintf("%d historical conflict patches", len(result.Uncertain))).TextColor(t.Warning).OnClick(func() {
				if !w.featureHistoryOpen {
					w.historicalQuery = ""
					w.historicalRow = 0
				}
				w.featureHistoryOpen = !w.featureHistoryOpen
			})
		}
		iconButton(c, iconSearch, "Find in diffs (⌘F)").OnClick(func() { w.finding = !w.finding })
		wrap := iconButton(c, iconWrap, "Toggle word wrap (⌥Z)").OnClick(func() { w.settings.WordWrap = !w.settings.WordWrap; w.rowsDirty = true })
		if w.settings.WordWrap {
			wrap.Background(pal.hover).TextColor(t.Text)
		}
		comments := iconButton(c, iconComment, "Review comments ("+strconv.Itoa(w.pendingComments())+")").OnClick(w.toggleReviewComments)
		if w.reviewCommentsOpen {
			comments.Background(pal.hover).TextColor(t.Text)
		}
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

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

var chooseReviewImportFile = mygo.Dialog.Open
var chooseReviewExportFile = mygo.Dialog.Save

type reviewImportConflict struct {
	reviewstore.CommentConflict
	choice string
}

type reviewImportState struct {
	file      reviewstore.ReviewFile
	remote    string
	gen       int
	conflicts []reviewImportConflict
}

func (w *window) reviewFilesMenu(c *ui.Context) {
	ready := w.isFeatureReview() && w.featureDB != nil && !w.loading && !w.featureClosing
	hasComments := w.pendingComments() > 0
	iconButton(c, iconOpen, "Review files…").Menu(func(m *ui.Menu) {
		if m.Item("Import review…").Disabled(!ready || w.reviewImportReading).Chosen() {
			w.importReviewFile()
		}
		if m.Item("Export review…").Disabled(!ready || !hasComments).Chosen() {
			w.exportReviewFile(false)
		}
		m.Separator()
		if m.Item("Export Markdown…").Disabled(!ready || !hasComments).Chosen() {
			w.exportReviewFile(true)
		}
		if m.Item("Copy Markdown").Disabled(!hasComments).Chosen() {
			w.copyComments()
		}
	})
}

func (w *window) exportReviewFile(markdown bool) {
	if w.featureDB == nil || !w.isFeatureReview() || w.loading || w.pendingComments() == 0 {
		return
	}
	w.saveFeatureReview()
	record, text := w.featureRecordSnapshot(), w.commentsMarkdown()
	parent, repo := w.win, w.repo
	w.background(func() {
		extension, kind := "json", "Sift review"
		if markdown {
			extension, kind = "md", "Markdown"
		}
		path, err := chooseReviewExportFile(mygo.SaveDialogOptions{Parent: parent, Title: "Export " + kind, DefaultPath: "sift-review." + extension, Filters: []mygo.FileFilter{{Name: kind, Extensions: []string{extension}}}})
		if err == nil && path != "" {
			if filepath.Ext(path) == "" {
				path += "." + extension
			}
			if markdown {
				err = os.WriteFile(path, []byte(text), 0o600)
			} else {
				remote, _ := repo.Git("remote", "get-url", "origin")
				file := reviewstore.ExportReview(record, filepath.Base(repo.Root), string(remote))
				err = reviewstore.WriteReview(path, file)
			}
		}
		if err != nil {
			w.update(func() { w.reviewFileError = "Could not export the review: " + err.Error() })
		}
	})
}

func (w *window) importReviewFile() {
	if w.loading || !w.isFeatureReview() || w.featureDB == nil || w.reviewImportReading {
		return
	}
	w.reviewImportReading = true
	parent, gen, repo := w.win, w.gen, w.repo
	w.background(func() {
		paths, err := chooseReviewImportFile(mygo.OpenDialogOptions{Parent: parent, Title: "Import review", Filters: []mygo.FileFilter{{Name: "Sift review", Extensions: []string{"json"}}}})
		var file reviewstore.ReviewFile
		if err == nil && len(paths) > 0 {
			var input *os.File
			input, err = os.Open(paths[0])
			if err == nil {
				file, err = reviewstore.ReadReview(input)
				input.Close()
			}
		}
		remote, _ := repo.Git("remote", "get-url", "origin")
		w.update(func() {
			w.reviewImportReading = false
			if w.stale(gen) || w.featureClosing {
				return
			}
			if err != nil {
				w.reviewFileError = "Could not import the review: " + err.Error()
				return
			}
			if len(paths) > 0 {
				w.prepareReviewImport(file, reviewstore.RepositoryRemote(string(remote)))
			}
		})
	})
}

func (w *window) prepareReviewImport(file reviewstore.ReviewFile, remote string) {
	state := &reviewImportState{file: file, remote: remote, gen: w.gen}
	local := w.featureRecordSnapshot()
	for _, conflict := range reviewstore.CommentConflicts(local.Comments, file.Comments) {
		state.conflicts = append(state.conflicts, reviewImportConflict{CommentConflict: conflict, choice: "Keep local"})
	}
	w.reviewImport = state
}

func (w *window) reviewImportBlocked(state *reviewImportState) string {
	if w.loading || w.featureClosing || state.gen != w.gen || !w.isFeatureReview() {
		return "The current review changed. Close this dialog and import again."
	}
	branch, historical, _ := strings.Cut(w.featureRecord.Scope.Branch, "::historical:")
	if state.file.Scope.Branch != branch {
		return "Switch to " + state.file.Scope.Branch + " in the header, then import again."
	}
	if state.file.Scope.Historical != historical {
		if state.file.Scope.Historical != "" {
			return "Open historical patch " + shortHash(state.file.Scope.Historical) + " before importing this review."
		}
		return "Return to the feature review before importing this file."
	}
	return ""
}

func (w *window) applyReviewImport() {
	state := w.reviewImport
	if state == nil || w.reviewImportBlocked(state) != "" {
		return
	}
	local := w.featureRecordSnapshot()
	replace := map[string]bool{}
	for _, conflict := range state.conflicts {
		replace[conflict.Imported.ID] = conflict.choice == "Use imported"
	}
	local.Comments = reviewstore.MergeComments(local.Comments, state.file.Comments, replace)
	w.featureRecord = local
	w.comments = nil
	w.restoreFeatureComments()
	w.rowsDirty = true
	w.saveFeatureReview()
	w.reviewImport = nil
	w.reviewCommentsOpen = true
	if list := w.orderedComments(); len(list) > 0 {
		w.revealComment(list[0])
	}
}

func (w *window) reviewFileDialogs(c *ui.Context) {
	if w.reviewFileError != "" {
		open := true
		if ui.AlertDialog(c, &open, "Review file error", w.reviewFileError, "OK") >= 0 || !open {
			w.reviewFileError = ""
		}
	}
	state := w.reviewImport
	if state == nil {
		return
	}
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		t := c.Theme()
		width, height := c.Size()
		panel.Width(min(560, width-32)).MaxHeight(min(680, height-32)).Padding(18).Gap(10).Radius(10).Border(1, t.Border).Background(paletteFor(t).headerBg)
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Import review").FontSize(17).Bold().Grow(1)
			iconButton(c, iconClose, "Close import").OnClick(func() { w.reviewImport = nil })
		})
		ui.Text(c, "Import into "+filepath.Base(w.repo.Root)).FontSize(12).FontWeight(600)
		ui.Text(c, "Branch: "+state.file.Scope.Branch).FontSize(12).SingleLine().FillWidth()
		ui.Text(c, fmt.Sprintf("Comments: %d", len(state.file.Comments))).FontSize(12)
		ui.Text(c, "Revision: "+shortHash(state.file.Revision)).Font(w.codeFont()).FontSize(12)
		if state.file.Repository.Remote != "" && state.remote != "" && state.file.Repository.Remote != state.remote {
			ui.Text(c, "The repository origin differs. Check that the selected repository is correct.").FontSize(12).TextColor(t.Warning).FillWidth()
		} else if state.file.Repository.Name != filepath.Base(w.repo.Root) {
			ui.Text(c, "Exported from "+state.file.Repository.Name+". Its local directory may have a different name.").FontSize(11).TextColor(t.TextMuted).FillWidth()
		}
		blocked := w.reviewImportBlocked(state)
		if blocked != "" {
			ui.Text(c, blocked).FontSize(12).TextColor(t.Warning).FillWidth()
		} else if state.file.Revision != w.featureRecord.Tip || state.file.Scope.Start != w.featureRecord.Scope.Start || state.file.Scope.Integration != w.featureRecord.Scope.Integration {
			ui.Text(c, "Version differs. Comments will be relocated by code context; missing locations will be kept separately.").FontSize(12).TextColor(t.Warning).FillWidth()
		} else {
			ui.Text(c, "Version matches the current review.").FontSize(12).TextColor(t.TextMuted)
		}
		if len(state.conflicts) > 0 {
			ui.Text(c, fmt.Sprintf("%d comments have changes. Choose which copy to keep.", len(state.conflicts))).FontSize(12).FillWidth()
			ui.Scroll(c).MaxHeight(260).FillWidth().Children(func() {
				ui.Column(c).Gap(12).FillWidth().Children(func() {
					for i := range state.conflicts {
						conflict := &state.conflicts[i]
						c.Key(conflict.Imported.ID)
						ui.Column(c).Gap(5).FillWidth().Padding(10).Border(1, t.Border).Radius(6).Children(func() {
							ui.Text(c, conflict.Local.Path).FontSize(12).Bold().SingleLine().FillWidth()
							ui.Text(c, "Local: "+conflict.Local.Body).FontSize(12).FillWidth().Selectable()
							ui.Text(c, "Imported: "+conflict.Imported.Body).FontSize(12).FillWidth().Selectable()
							ui.Select(c, &conflict.choice, []string{"Keep local", "Use imported"}).Label("Conflict " + conflict.Imported.ID)
						})
					}
				})
			})
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			ui.Button(c, "Cancel").OnClick(func() { w.reviewImport = nil })
			ui.PrimaryButton(c, "Import").Disabled(blocked != "").OnClick(w.applyReviewImport)
		})
	})
	if !open {
		w.reviewImport = nil
	}
}

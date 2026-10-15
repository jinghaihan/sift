package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/review"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

var chooseReviewImportFile = mygo.Dialog.Open
var chooseReviewExportFile = mygo.Dialog.Save

func (w *window) reviewFileActions(c *ui.Context) {
	ready := w.isFeatureReview() && w.featureDB != nil && !w.loading && !w.featureClosing
	hasComments := w.pendingComments() > 0
	iconButton(c, iconUpload, "Import review…").Disabled(!ready || w.reviewImportReading).OnClick(w.importReviewFile)
	iconButton(c, iconShare, "Export review…").Disabled(!ready || !hasComments).Menu(func(m *ui.Menu) {
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
			w.update(func() { w.reviewFileErrorTitle, w.reviewFileError = "Export failed", err.Error() })
		}
	})
}

func (w *window) importReviewFile() {
	if w.loading || !w.isFeatureReview() || w.featureDB == nil || w.reviewImportReading {
		return
	}
	w.reviewImportReading = true
	parent, gen, db := w.win, w.gen, w.featureDB
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
		w.update(func() {
			if w.stale(gen) || w.featureDB != db || w.featureClosing {
				w.reviewImportReading = false
				return
			}
			if err != nil {
				w.reviewImportReading = false
				w.reviewFileErrorTitle, w.reviewFileError = "Import failed", err.Error()
				return
			}
			if len(paths) == 0 {
				w.reviewImportReading = false
				return
			}
			w.restoreReviewFile(file)
		})
	})
}

func (w *window) reviewImportBlocked(file reviewstore.ReviewFile) string {
	branch, historical, _ := strings.Cut(w.featureRecord.Scope.Branch, "::historical:")
	if file.Scope.Branch != branch {
		return fmt.Sprintf("Branch mismatch.\n\nJSON branch: %s\nCurrent branch: %s", file.Scope.Branch, branch)
	}
	if file.Scope.Historical != historical {
		if file.Scope.Historical != "" {
			current := "Feature review"
			if historical != "" {
				current = "Historical patch " + shortHash(historical)
			}
			return fmt.Sprintf("This JSON belongs to a different historical patch.\n\nJSON patch: %s\nCurrent view: %s", shortHash(file.Scope.Historical), current)
		}
		return "This JSON contains the feature review, but a historical patch is currently open."
	}
	return ""
}

func (w *window) restoreReviewFile(file reviewstore.ReviewFile) {
	if blocked := w.reviewImportBlocked(file); blocked != "" {
		w.reviewImportReading = false
		w.reviewFileErrorTitle, w.reviewFileError = "Import failed", blocked
		return
	}
	current := w.featureRecordSnapshot()
	incoming := current.Clone()
	incoming.Comments = slices.Clone(file.Comments)
	incoming.Script = reviewstore.CloneScript(file.Script)
	for _, proof := range file.Provenance {
		if !slices.ContainsFunc(incoming.Provenance, func(p review.CommitProvenance) bool { return p.ID == proof.ID }) {
			incoming.Provenance = append(incoming.Provenance, proof)
		}
	}
	// Relocate imported anchors against the displayed revision on the UI
	// thread, without changing the active review before persistence succeeds.
	candidate := &window{featureRecord: incoming, files: w.files, viewed: w.viewed}
	candidate.restoreFeatureComments()
	incoming = candidate.featureRecordSnapshot()
	db, gen := w.featureDB, w.gen
	w.background(func() {
		record, err := db.Update(func(latest *reviewstore.Record) error {
			if w.stale(gen) || latest.Scope != current.Scope || latest.Tip != "" && latest.Tip != current.Tip {
				return reviewstore.ErrConcurrentWrite
			}
			// This is a full restore, including IDs absent from the JSON.
			// A sequence beyond queued autosaves prevents an older snapshot
			// from adding the replaced comments back after the import.
			latest.Sequence = max(latest.Sequence, current.Sequence)
			latest.Tip = current.Tip
			latest.Comments = incoming.Comments
			latest.Script = incoming.Script
			latest.Provenance = incoming.Provenance
			return nil
		})
		w.update(func() {
			w.reviewImportReading = false
			if w.stale(gen) || w.featureDB != db || w.featureClosing {
				return
			}
			if err != nil {
				w.reviewFileErrorTitle, w.reviewFileError = "Import failed", err.Error()
				return
			}
			w.featureRecord = record
			w.comments, w.discarding = candidate.comments, nil
			w.reviewCommentID = ""
			w.rowsDirty, w.reviewCommentsOpen = true, true
			if list := w.orderedComments(); len(list) > 0 {
				w.revealComment(list[0])
			}
			w.reviewFileNotice = "Imported " + plural(len(file.Comments), "comment")
		})
	})
}

func (w *window) reviewFileDialogs(c *ui.Context) {
	if w.reviewFileNotice != "" {
		c.Toast(w.reviewFileNotice)
		w.reviewFileNotice = ""
	}
	if w.reviewFileError != "" {
		open := true
		if ui.AlertDialog(c, &open, w.reviewFileErrorTitle, w.reviewFileError, "OK") >= 0 || !open {
			w.reviewFileError = ""
		}
	}
}

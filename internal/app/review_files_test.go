package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

func reviewMenuItem(t *testing.T, tt *ui.Tester, item string) {
	t.Helper()
	tt.Frame()
	if err := tt.Click("Review files…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
}

func reviewFileDialogsForTest(t *testing.T, importPath, exportPath string) {
	t.Helper()
	oldOpen, oldSave := chooseReviewImportFile, chooseReviewExportFile
	t.Cleanup(func() { chooseReviewImportFile, chooseReviewExportFile = oldOpen, oldSave })
	chooseReviewImportFile = func(opts mygo.OpenDialogOptions) ([]string, error) {
		if len(opts.Filters) != 1 || opts.Filters[0].Extensions[0] != "json" {
			t.Fatal("import did not use a review file picker")
		}
		if importPath == "" {
			return nil, nil
		}
		return []string{importPath}, nil
	}
	chooseReviewExportFile = func(mygo.SaveDialogOptions) (string, error) { return exportPath, nil }
}

func TestReviewFilesExportImportAndConflictChoices(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep the original review note."
	w.saveFeatureReview()
	id, created := w.comments[0].storeID, w.comments[0].created
	filePath := filepath.Join(t.TempDir(), "review.json")
	reviewFileDialogsForTest(t, filePath, filePath)
	reviewMenuItem(t, tt, "Export review…")
	input, err := os.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := reviewstore.ReadReview(input)
	input.Close()
	if err != nil || len(file.Comments) != 1 || file.Comments[0].ID != id || !file.Comments[0].Created.Equal(created) {
		t.Fatalf("invalid export: %+v, %v", file, err)
	}
	data, _ := os.ReadFile(filePath)
	if strings.Contains(string(data), w.repo.Root) {
		t.Fatal("review export contains a local repository path")
	}
	file.Comments[0].Body = "Use this updated explanation during CR."
	if err := reviewstore.WriteReview(filePath, file); err != nil {
		t.Fatal(err)
	}
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewImport == nil || len(w.reviewImport.conflicts) != 1 || !tt.HasText("Version matches the current review.") {
		t.Fatal("import did not show the version and changed comment")
	}
	snapshot(t, tt, "review-import-conflict")
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if w.reviewImport != nil || w.comments[0].text != "Keep the original review note." {
		t.Fatal("canceling import changed a comment")
	}
	reviewMenuItem(t, tt, "Import review…")
	if err := tt.Click("Conflict " + id); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Use imported"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Import"); err != nil {
		t.Fatal(err)
	}
	if w.reviewImport != nil || !w.reviewCommentsOpen || len(w.comments) != 1 || w.comments[0].text != file.Comments[0].Body || !w.comments[0].created.Equal(created) {
		t.Fatal("import did not apply the explicit conflict choice")
	}
	reviewMenuItem(t, tt, "Import review…")
	if len(w.reviewImport.conflicts) != 0 {
		t.Fatal("identical import reported a conflict")
	}
	tt.Click("Import")
	if len(w.comments) != 1 {
		t.Fatal("repeated import duplicated a comment")
	}
	w.comments = nil
	w.restoreFeatureComments()
	if len(w.comments) != 1 || w.comments[0].text != file.Comments[0].Body {
		t.Fatal("import did not save a restorable review")
	}
	markdownPath := filepath.Join(t.TempDir(), "review.md")
	chooseReviewExportFile = func(mygo.SaveDialogOptions) (string, error) { return markdownPath, nil }
	reviewMenuItem(t, tt, "Export Markdown…")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil || !strings.Contains(string(markdown), file.Comments[0].Body) || !strings.Contains(string(markdown), "main.go") {
		t.Fatalf("Markdown export failed: %s, %v", markdown, err)
	}
}

func TestImportIntoDifferentCheckoutKeepsMissingLocations(t *testing.T) {
	w, tt := newFeatureReview(t)
	cm := reviewstore.NewComment("removed.go", "removed.go", "Explain this earlier decision.", reviewstore.Anchor{Side: 1, Start: 7, End: 7, Excerpt: "removedCall()"})
	record := w.featureRecord.Clone()
	record.Scope.Repository = "/another/computer/checkout"
	record.Tip = strings.Repeat("a", 40)
	record.Comments = []reviewstore.Comment{cm}
	file := reviewstore.ExportReview(record, "another-directory", "")
	path := filepath.Join(t.TempDir(), "review.json")
	if err := reviewstore.WriteReview(path, file); err != nil {
		t.Fatal(err)
	}
	reviewFileDialogsForTest(t, path, "")
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewImport == nil || !tt.HasText("Version differs. Comments will be relocated by code context; missing locations will be kept separately.") {
		t.Fatal("changed revision was not explained")
	}
	tt.Click("Import")
	if len(w.comments) != 1 || !w.comments[0].outdated || !tt.HasText("Location changed") {
		t.Fatal("missing location was not kept for review")
	}
	if w.featureRecord.Scope.Repository != w.repo.Root || w.featureRecord.Scope.Branch != "feature" {
		t.Fatal("import replaced the selected local repository or branch")
	}
	if got := w.featureRecord.Comments[0].Anchor; got != cm.Anchor {
		t.Fatal("import lost original context")
	}
}

func TestImportRejectsWrongBranchAndInvalidFiles(t *testing.T) {
	w, tt := newFeatureReview(t)
	file := reviewstore.ExportReview(w.featureRecord, "example", "")
	file.Scope.Branch = "another-feature"
	path := filepath.Join(t.TempDir(), "review.json")
	if err := reviewstore.WriteReview(path, file); err != nil {
		t.Fatal(err)
	}
	reviewFileDialogsForTest(t, path, "")
	reviewMenuItem(t, tt, "Import review…")
	if !tt.HasText("Switch to another-feature in the header, then import again.") {
		t.Fatal("wrong branch did not prompt for a switch")
	}
	tt.Click("Import")
	if w.reviewImport == nil || len(w.comments) != 0 {
		t.Fatal("wrong branch was imported")
	}
	tt.Click("Cancel")
	os.WriteFile(path, []byte(`{"format":"sift-review","version":999}`), 0o600)
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewImport != nil || w.reviewFileError == "" || len(w.comments) != 0 {
		t.Fatal("invalid import mutated the review")
	}
	tt.Click("OK")
	chooseReviewImportFile = func(mygo.OpenDialogOptions) ([]string, error) { return nil, nil }
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewImport != nil || w.reviewImportReading {
		t.Fatal("canceling the file picker changed the review")
	}
}

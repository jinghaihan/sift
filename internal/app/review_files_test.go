package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	reviewstore "github.com/jinghaihan/sift/internal/store"
)

func reviewMenuItem(t *testing.T, tt *ui.Tester, item string) {
	t.Helper()
	tt.Frame()
	if _, ok := tt.Find("Import review…"); !ok {
		for _, label := range tt.Texts() {
			if strings.HasPrefix(label, "Review comments (") {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	if item == "Import review…" {
		if err := tt.Click(item); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := tt.Click("Export review…"); err != nil {
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

func TestReviewFilesRestoreJSONDirectly(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep the original review note."
	w.featureRecord.Script = []reviewstore.ScriptSection{{ID: "intro", Title: "Original", Body: "Original script."}}
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
	file.Script[0].Body = "Use the imported script."
	if err := reviewstore.WriteReview(filePath, file); err != nil {
		t.Fatal(err)
	}

	// The JSON must replace both same-ID content and local-only entries,
	// including a save that was queued before the file was chosen.
	w.addComment("long.txt", sideNew, 250, 250)
	w.comments[1].text = "This note is absent from the exported file."
	w.featureRecord.Script = append(w.featureRecord.Script, reviewstore.ScriptSection{ID: "local-only", Title: "Local", Body: "Not in the file."})
	w.hold = true
	w.saveFeatureReview()
	pendingSave := w.held[len(w.held)-1]
	w.held, w.hold = nil, false
	otherScope := w.featureRecord.Scope
	otherScope.Branch = "another-feature"
	otherDB, other, err := reviewstore.Open(filepath.Dir(cfg.path), otherScope)
	if err != nil {
		t.Fatal(err)
	}
	other.Comments = []reviewstore.Comment{reviewstore.NewComment("other.go", "other.go", "Keep the other branch.", file.Comments[0].Anchor)}
	other.Sequence++
	if err := otherDB.Save(other); err != nil {
		t.Fatal(err)
	}

	reviewMenuItem(t, tt, "Import review…")
	if w.reviewImportReading || w.reviewFileError != "" || !w.reviewCommentsOpen || len(w.comments) != 1 || w.comments[0].text != file.Comments[0].Body || !w.comments[0].created.Equal(created) {
		t.Fatalf("selecting JSON did not restore it directly: %s", w.reviewFileError)
	}
	if len(w.featureRecord.Script) != 1 || w.featureRecord.Script[0].Body != file.Script[0].Body {
		t.Fatal("JSON did not replace the script exactly")
	}
	if !tt.HasText("Imported 1 comment") || tt.HasText("Import review confirmation") || tt.HasText("Keep local") {
		t.Fatal("import should show a success toast, without a confirmation form")
	}
	if os.Getenv("SIFT_SNAPSHOTS") != "" {
		time.Sleep(250 * time.Millisecond)
		tt.Frame()
	}
	snapshot(t, tt, "review-import-simple-header-success")
	pendingSave()
	_, saved, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil || len(saved.Comments) != 1 || saved.Comments[0].Body != file.Comments[0].Body || len(saved.Script) != 1 {
		t.Fatalf("an older autosave undid the JSON restore: %+v, %v", saved, err)
	}
	_, untouched, err := reviewstore.Open(filepath.Dir(cfg.path), otherScope)
	if err != nil || len(untouched.Comments) != 1 || untouched.Comments[0].ID != other.Comments[0].ID {
		t.Fatal("import modified a different branch's review")
	}
	reviewMenuItem(t, tt, "Import review…")
	if len(w.comments) != 1 {
		t.Fatal("repeated import duplicated a comment")
	}
	markdownPath := filepath.Join(t.TempDir(), "review.md")
	chooseReviewExportFile = func(mygo.SaveDialogOptions) (string, error) { return markdownPath, nil }
	reviewMenuItem(t, tt, "Export Markdown…")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil || !strings.Contains(string(markdown), file.Comments[0].Body) || !strings.Contains(string(markdown), "main.go") {
		t.Fatalf("Markdown export failed: %s, %v", markdown, err)
	}
	file.Comments, file.Script = nil, nil
	if err := reviewstore.WriteReview(filePath, file); err != nil {
		t.Fatal(err)
	}
	reviewMenuItem(t, tt, "Import review…")
	_, empty, err := reviewstore.Open(filepath.Dir(cfg.path), w.featureRecord.Scope)
	if err != nil || len(w.comments) != 0 || len(empty.Comments) != 0 || len(empty.Script) != 0 {
		t.Fatal("an empty JSON review did not clear current notes and script")
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
	if !tt.HasText("Branch mismatch.\n\nJSON branch: another-feature\nCurrent branch: feature") || len(w.comments) != 0 {
		t.Fatal("wrong branch was not rejected without changing the current review")
	}
	snapshot(t, tt, "review-import-simple-header-error")
	tt.Click("OK")
	os.WriteFile(path, []byte(`{"format":"sift-review","version":999}`), 0o600)
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewFileError == "" || len(w.comments) != 0 {
		t.Fatal("invalid import mutated the review")
	}
	tt.Click("OK")
	chooseReviewImportFile = func(mygo.OpenDialogOptions) ([]string, error) { return nil, nil }
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewFileError != "" || w.reviewImportReading {
		t.Fatal("canceling the file picker changed the review")
	}
}

func TestImportFailureAndObsoleteJobsKeepCurrentReview(t *testing.T) {
	w, tt := newFeatureReview(t)
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Keep this until restore succeeds."
	w.saveFeatureReview()
	file := reviewstore.ExportReview(w.featureRecord, "example", "")
	file.Comments = nil
	path := filepath.Join(t.TempDir(), "review.json")
	if err := reviewstore.WriteReview(path, file); err != nil {
		t.Fatal(err)
	}
	reviewFileDialogsForTest(t, path, "")
	w.hold = true
	w.importReviewFile()
	oldJob := w.held[0]
	w.held, w.hold = nil, false
	w.gen++
	w.genA.Store(int64(w.gen))
	oldJob()
	if w.reviewImportReading || w.reviewFileError != "" || len(w.comments) != 1 {
		t.Fatal("an obsolete picker result changed the active review")
	}
	broken := filepath.Join(t.TempDir(), "store")
	db, _, err := reviewstore.Open(broken, w.featureRecord.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.featureDB = db
	reviewMenuItem(t, tt, "Import review…")
	if w.reviewFileError == "" || len(w.comments) != 1 || w.comments[0].text != "Keep this until restore succeeds." {
		t.Fatal("a failed disk restore changed the active review")
	}
}

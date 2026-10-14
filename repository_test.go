package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/jinghaihan/sift/internal/git"
)

func TestRepositoryNameOpensFolderPicker(t *testing.T) {
	oldSelect, oldOpen := selectRepositoryFolder, openSelectedRepository
	t.Cleanup(func() { selectRepositoryFolder, openSelectedRepository = oldSelect, oldOpen })
	root := t.TempDir()
	selected := filepath.Join(t.TempDir(), "another-project")
	var options mygo.OpenDialogOptions
	var opened string
	selectRepositoryFolder = func(value mygo.OpenDialogOptions) ([]string, error) { options = value; return []string{selected}, nil }
	openSelectedRepository = func(path string, src source) error { opened = path; return nil }
	w := newWindow(&git.Repo{Root: root}, source{kind: sourceFeature, ref: "feature"})
	tt := ui.NewTester(w.view, 1280, 860)
	if err := tt.Click("Open repository"); err != nil {
		t.Fatal(err)
	}
	if opened != selected || !options.Directory || options.DefaultPath != filepath.Dir(root) || options.Parent != w.win {
		t.Fatalf("picker did not open selected repository: %q %+v", opened, options)
	}
	opened = ""
	selectRepositoryFolder = func(mygo.OpenDialogOptions) ([]string, error) { return nil, nil }
	if err := tt.Click("Open repository"); err != nil {
		t.Fatal(err)
	}
	if opened != "" || w.repo.Root != root {
		t.Fatal("canceling changed the review repository")
	}
}

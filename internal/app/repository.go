package app

import (
	"path/filepath"

	"github.com/egoist/mygo"
)

var selectRepositoryFolder = mygo.Dialog.Open
var openSelectedRepository func(string, source) error

func init() {
	openSelectedRepository = openWindow
}

// chooseRepository attaches the picker to the current review window.
func (w *window) chooseRepository() {
	parent, directory := w.win, filepath.Dir(w.repo.Root)
	w.background(func() {
		paths, err := selectRepositoryFolder(mygo.OpenDialogOptions{
			Parent: parent, Title: "Choose Git repository", Directory: true,
			DefaultPath: directory, ButtonLabel: "Open repository",
		})
		if err == nil && len(paths) > 0 {
			err = openSelectedRepository(paths[0], source{kind: sourceWorkingTree})
		}
		if err != nil {
			mygo.Dialog.Message(mygo.MessageOptions{Parent: parent, Type: mygo.MessageError, Title: "Could not open repository", Message: errorText(err)})
		}
	})
}

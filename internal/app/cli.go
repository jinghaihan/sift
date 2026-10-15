package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// takeCwd takes the --cwd option out of a command line: the directory the
// sift command ran in, which `open` does not pass on.
func takeCwd(args []string, wd string) ([]string, string) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cwd" && i+1 < len(args) {
			wd = args[i+1]
			i++
			continue
		}
		if v, ok := strings.CutPrefix(args[i], "--cwd="); ok {
			wd = v
			continue
		}
		out = append(out, args[i])
	}
	return out, wd
}

// appBundle returns the .app holding the running executable, "" outside
// one.
func appBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	for dir := filepath.Dir(exe); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}

// cliScript is the sift command: it opens the app on the repository it
// runs in, as codiff's terminal helper does.
func cliScript() string {
	exe, _ := os.Executable()
	return commandScript(exe, appBundle(), runtime.GOOS)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func commandScript(exe, bundle, platform string) string {
	prefix := "#!/bin/sh\ncase \"$1\" in\n  analyze|draft|validate|import|export|script|help) exec " + shellQuote(exe) + " \"$@\" ;;\nesac\n"
	if bundle != "" && platform == "darwin" {
		return prefix + "exec open -n -a " + shellQuote(bundle) + " --args --cwd \"$PWD\" \"$@\"\n"
	}
	return prefix + shellQuote(exe) + " --cwd \"$PWD\" \"$@\" >/dev/null 2>&1 &\n"
}

// installCLI writes the sift command into a directory of the PATH.
func installCLI() {
	go func() {
		home, _ := os.UserHomeDir()
		dirs := []string{"/usr/local/bin", "/opt/homebrew/bin", filepath.Join(home, ".local", "bin")}
		var errs []string
		for _, dir := range dirs {
			if _, err := os.Stat(dir); err != nil && dir != dirs[len(dirs)-1] {
				continue
			}
			os.MkdirAll(dir, 0o755)
			path := filepath.Join(dir, "sift")
			if err := os.WriteFile(path, []byte(cliScript()), 0o755); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			os.Chmod(path, 0o755)
			mygo.Dialog.Message(mygo.MessageOptions{
				Type:    mygo.MessageInfo,
				Message: "The sift command is installed",
				Detail:  "It is at " + path + ". Run sift in a Git repository to review its changes, or sift <commit> and sift <branch>.",
			})
			return
		}
		mygo.Dialog.Error("Could not install the sift command", strings.Join(errs, "\n"))
	}()
}

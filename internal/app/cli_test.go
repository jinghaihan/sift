package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstalledCommandPreservesHeadlessOutputAndArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell helper")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "Sift's $(touch unwanted).bin")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "sift")
	if err := os.WriteFile(wrapper, []byte(commandScript(executable, "/Applications/Sift.app", "darwin")), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", wrapper, "help", "--repo", "repo with spaces")
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	if err != nil || string(raw) != "help\n--repo\nrepo with spaces\n" {
		t.Fatalf("headless helper opened the app or lost stdout/arguments: %s %v", raw, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "unwanted")); !os.IsNotExist(err) {
		t.Fatal("executable path was interpreted as shell code")
	}
	if !strings.Contains(commandScript(executable, "/Applications/Sift.app", "darwin"), "exec open -n -a") {
		t.Fatal("normal GUI launch was removed")
	}
}

package app

import (
	"fmt"
	"path"
	"strings"
)

type fileExclusion struct {
	Pattern string `json:"pattern"`
	Enabled bool   `json:"enabled"`
}

// A ** segment matches zero or more path segments. Other segments use Go's
// ordinary glob syntax (*, ?, character classes, and escaped characters).
type pathGlob []string

func compilePathGlob(pattern string) (pathGlob, error) {
	anchored := strings.HasPrefix(strings.TrimSpace(pattern), "/")
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "/")
	if pattern == "" {
		return nil, fmt.Errorf("Enter a file pattern.")
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("Invalid glob pattern.")
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	if !anchored && !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	return strings.Split(pattern, "/"), nil
}

func (g pathGlob) matches(name string) bool {
	parts := strings.Split(name, "/")
	type position struct{ pattern, file int }
	seen, values := map[position]bool{}, map[position]bool{}
	var match func(int, int) bool
	match = func(p, f int) bool {
		key := position{p, f}
		if seen[key] {
			return values[key]
		}
		seen[key] = true
		result := false
		switch {
		case p == len(g):
			result = f == len(parts)
		case g[p] == "**":
			result = match(p+1, f) || f < len(parts) && match(p, f+1)
		case f < len(parts):
			ok, _ := path.Match(g[p], parts[f])
			result = ok && match(p+1, f+1)
		}
		values[key] = result
		return result
	}
	return match(0, 0)
}

func (w *window) compileFileExclusions() {
	w.exclusionMatchers = nil
	w.exclusionPaths = map[string]bool{}
	for _, rule := range w.settings.FileExclusions {
		if rule.Enabled {
			if glob, err := compilePathGlob(rule.Pattern); err == nil {
				w.exclusionMatchers = append(w.exclusionMatchers, glob)
			}
		}
	}
}

func (w *window) fileAllowed(name string) bool {
	if w.showAllFiles || name == w.revealedExcludedPath {
		return true
	}
	if w.exclusionPaths == nil {
		w.exclusionPaths = map[string]bool{}
	}
	if excluded, ok := w.exclusionPaths[name]; ok {
		return !excluded
	}
	for _, glob := range w.exclusionMatchers {
		if glob.matches(name) {
			w.exclusionPaths[name] = true
			return false
		}
	}
	w.exclusionPaths[name] = false
	return true
}

func (w *window) hiddenFileCount() int {
	count := 0
	for _, f := range w.files {
		if w.hasContent(f) && !w.fileAllowed(f.Path) {
			count++
		}
	}
	return count
}

func (w *window) refreshFileFilters() {
	w.fileFiltersDirty = false
	w.matchesFor = "\x00"
	w.updateMatches()
	w.buildTree()
	w.rowsDirty = true
}

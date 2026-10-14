package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jinghaihan/sift/internal/review"
)

type projectSession struct {
	Repository string                  `json:"repository"`
	LastBranch string                  `json:"lastBranch"`
	Scopes     map[string]review.Scope `json:"scopes"`
}

// Serialize read-modify-write operations when windows review different branches
// of the same repository. Each repository has an independent session file.
var scopeMu sync.Mutex

func sessionPath(directory, repository string) string {
	return filepath.Join(directory, "sessions", Fingerprint(repository)[:24]+".json")
}

func readProjectSession(directory, repository string) (projectSession, error) {
	session := projectSession{Repository: repository, Scopes: map[string]review.Scope{}}
	data, err := os.ReadFile(sessionPath(directory, repository))
	if errors.Is(err, os.ErrNotExist) {
		return session, nil
	}
	if err != nil {
		return session, err
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return session, fmt.Errorf("read saved review ranges: %w", err)
	}
	if session.Repository != repository {
		return session, fmt.Errorf("saved review ranges belong to another repository")
	}
	for branch, scope := range session.Scopes {
		if scope.Repository != repository || scope.Branch != branch || !completeScope(scope) {
			return session, fmt.Errorf("invalid saved review range for %s", branch)
		}
	}
	if session.Scopes == nil {
		session.Scopes = map[string]review.Scope{}
	}
	return session, nil
}

func completeScope(scope review.Scope) bool {
	return scope.Repository != "" && scope.Branch != "" && scope.Integration != "" && scope.Start != ""
}

func legacyScope(directory string) (review.Scope, error) {
	var scope review.Scope
	data, err := os.ReadFile(filepath.Join(directory, "session.json"))
	if errors.Is(err, os.ErrNotExist) {
		return scope, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &scope)
	}
	return scope, err
}

func saveProjectScope(directory string, session projectSession, scope review.Scope) error {
	session.Scopes[scope.Branch] = scope
	session.LastBranch = scope.Branch
	return writeJSON(sessionPath(directory, scope.Repository), session)
}

// SaveScope remembers every branch's range and the last branch of its project.
func SaveScope(directory string, scope review.Scope) error {
	if !completeScope(scope) {
		return fmt.Errorf("cannot save an incomplete review range")
	}
	scopeMu.Lock()
	defer scopeMu.Unlock()
	session, err := readProjectSession(directory, scope.Repository)
	if err != nil {
		return err
	}
	return saveProjectScope(directory, session, scope)
}

// LastBranch restores the project's last selection without borrowing another
// project's session. Old installations keep their existing last selection.
func LastBranch(directory, repository string) (string, error) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	session, err := readProjectSession(directory, repository)
	if err != nil || session.LastBranch != "" {
		return session.LastBranch, err
	}
	old, err := legacyScope(directory)
	if err == nil && old.Repository == repository && completeScope(old) {
		err = saveProjectScope(directory, session, old)
		return old.Branch, err
	}
	return "", err
}

// LoadScope restores only the selected project and branch. Migrate the old
// global session or recover a unique scope from existing review records, so
// switching projects does not disconnect previously saved comments.
func LoadScope(directory, repository, branch string) (review.Scope, error) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	session, err := readProjectSession(directory, repository)
	if err != nil {
		return review.Scope{}, err
	}
	if scope, ok := session.Scopes[branch]; ok {
		return scope, nil
	}
	scope, err := legacyScope(directory)
	if err != nil {
		return review.Scope{}, err
	}
	if scope.Repository != repository || scope.Branch != branch || !completeScope(scope) {
		scope, err = recoverScope(directory, repository, branch)
		if err != nil || scope.Start == "" {
			return scope, err
		}
	}
	return scope, saveProjectScope(directory, session, scope)
}

func recoverScope(directory, repository, branch string) (review.Scope, error) {
	var found review.Scope
	entries, err := os.ReadDir(filepath.Join(directory, "reviews"))
	if errors.Is(err, os.ErrNotExist) {
		return found, nil
	}
	if err != nil {
		return found, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, "reviews", entry.Name()))
		if err != nil {
			return found, err
		}
		var record Record
		if json.Unmarshal(data, &record) != nil {
			continue
		}
		scope := record.Scope
		if scope.Repository != repository || scope.Branch != branch || !completeScope(scope) {
			continue
		}
		if found.Start != "" && found != scope {
			return review.Scope{}, fmt.Errorf("multiple saved review ranges for %s; choose the review range", branch)
		}
		found = scope
	}
	return found, nil
}

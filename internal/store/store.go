package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jinghaihan/sift/internal/review"
)

type Anchor struct {
	Side    int    `json:"side"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Excerpt string `json:"excerpt"`
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

type Comment struct {
	ID       string    `json:"id"`
	File     string    `json:"file"`
	Path     string    `json:"path"`
	Anchor   Anchor    `json:"anchor"`
	Body     string    `json:"body"`
	Resolved bool      `json:"resolved"`
	Outdated bool      `json:"outdated"`
	Created  time.Time `json:"created"`
}

type Record struct {
	Scope      review.Scope              `json:"scope"`
	Tip        string                    `json:"tip"`
	Sequence   uint64                    `json:"sequence"`
	Comments   []Comment                 `json:"comments"`
	Viewed     map[string]string         `json:"viewed"`
	Provenance []review.CommitProvenance `json:"provenance,omitempty"`
}

func (r Record) Clone() Record {
	r.Provenance = slices.Clone(r.Provenance)
	r.Comments = slices.Clone(r.Comments)
	r.Viewed = maps.Clone(r.Viewed)
	return r
}

func Fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func NewComment(file, path, body string, anchor Anchor) Comment {
	return Comment{ID: rand.Text(), File: file, Path: path, Body: strings.TrimSpace(body), Anchor: anchor, Created: time.Now()}
}

type Database struct {
	path     string
	mu       sync.Mutex
	sequence uint64
}

func DefaultDirectory() (string, error) {
	root, err := os.UserConfigDir()
	return filepath.Join(root, "sift"), err
}

func Open(directory string, scope review.Scope) (*Database, Record, error) {
	key, _ := json.Marshal(scope)
	path := filepath.Join(directory, "reviews", Fingerprint(string(key))[:24]+".json")
	db := &Database{path: path}
	record := Record{Scope: scope, Viewed: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, record, nil
	}
	if err != nil {
		return nil, record, err
	}
	if err = json.Unmarshal(data, &record); err != nil {
		return nil, record, fmt.Errorf("read review: %w", err)
	}
	if record.Viewed == nil {
		record.Viewed = map[string]string{}
	}
	db.sequence = record.Sequence
	return db, record, nil
}

// Save rejects late background writes, so a slower save cannot overwrite a
// newer comment or a resolved thread.
func (db *Database) Save(record Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if record.Sequence < db.sequence {
		return nil
	}
	if err := writeJSON(db.path, record); err != nil {
		return err
	}
	db.sequence = record.Sequence
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sift-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(append(data, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Relocate finds an anchor in the refreshed, numbered patch context. Repeated
// snippets are not guessed; those comments remain visible as outdated.
func Relocate(anchor Anchor, lines map[int]string) (Anchor, bool) {
	text := strings.Split(anchor.Excerpt, "\n")
	matches := func(start int, neighbors bool) bool {
		for i, want := range text {
			got, ok := lines[start+i]
			if !ok || got != want {
				return false
			}
		}
		return !neighbors || (anchor.Before == "" || lines[start-1] == anchor.Before) && (anchor.After == "" || lines[start+len(text)] == anchor.After)
	}
	if matches(anchor.Start, true) {
		return anchor, true
	}
	var candidates []int
	for line := range lines {
		if matches(line, true) {
			candidates = append(candidates, line)
		}
	}
	if len(candidates) != 1 {
		return anchor, false
	}
	anchor.Start = candidates[0]
	anchor.End = anchor.Start + len(text) - 1
	return anchor, true
}

func Markdown(record Record) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Sift review\n\nBranch: `%s`\n\nIntegration: `%s`\n\nFirst feature commit: `%s`\n\nReviewed revision: `%s`\n", record.Scope.Branch, record.Scope.Integration, record.Scope.Start, record.Tip)
	for _, comment := range record.Comments {
		status := "Open"
		if comment.Resolved {
			status = "Resolved"
		}
		if comment.Outdated {
			status += " · Outdated"
		}
		side := "new"
		if comment.Anchor.Side == 0 {
			side = "old"
		}
		fmt.Fprintf(&out, "\n## %s:%d–%d (%s)\n\n%s\n\n", comment.Path, comment.Anchor.Start, comment.Anchor.End, side, status)
		fence := "```"
		for strings.Contains(comment.Anchor.Excerpt, fence) {
			fence += "`"
		}
		fmt.Fprintf(&out, "%s\n%s\n%s\n\n%s\n", fence, comment.Anchor.Excerpt, fence, comment.Body)
	}
	return out.String()
}

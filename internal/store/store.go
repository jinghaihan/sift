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
	Source   string    `json:"source,omitempty"`
}

type Record struct {
	Scope      review.Scope              `json:"scope"`
	Tip        string                    `json:"tip"`
	Sequence   uint64                    `json:"sequence"`
	Comments   []Comment                 `json:"comments"`
	Viewed     map[string]string         `json:"viewed"`
	Provenance []review.CommitProvenance `json:"provenance,omitempty"`
	Script     []ScriptSection           `json:"script,omitempty"`
}

func (r Record) Clone() Record {
	r.Script = CloneScript(r.Script)
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
	scope    review.Scope
	mu       sync.Mutex
	sequence uint64
	base     Record
}

func DefaultDirectory() (string, error) {
	if dir := os.Getenv("SIFT_DATA_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	root, err := os.UserHomeDir()
	return filepath.Join(root, ".sift"), err
}

func Open(directory string, scope review.Scope) (*Database, Record, error) {
	key, _ := json.Marshal(scope)
	path := filepath.Join(directory, "reviews", Fingerprint(string(key))[:24]+".json")
	db := &Database{path: path, scope: scope}
	record := Record{Scope: scope, Viewed: map[string]string{}}
	db.base = record.Clone()
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
	db.base = record.Clone()
	return db, record, nil
}

// Save merges this writer's changes under a cross-process lock. Concurrent
// changes to the same item fail explicitly; unrelated foreign drafts survive.
func (db *Database) Save(record Record) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if record.Sequence < db.sequence {
		return nil
	}
	release, err := lockRecord(db.path)
	if err != nil {
		return err
	}
	defer release()
	data, err := os.ReadFile(db.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	exists := err == nil
	current := Record{Scope: db.scope, Viewed: map[string]string{}}
	if err == nil {
		if err = json.Unmarshal(data, &current); err != nil {
			return err
		}
	}
	merged, err := mergeRecord(db.base, record, current)
	if err != nil {
		return err
	}
	if exists && equalRecord(merged, current) {
		db.base, db.sequence = record.Clone(), record.Sequence
		return nil
	}
	merged.Sequence = max(current.Sequence+1, record.Sequence)
	if err = writeJSON(db.path, merged); err != nil {
		return err
	}
	db.base, db.sequence = record.Clone(), record.Sequence

	return nil
}

var ErrConcurrentWrite = errors.New("review changed in another writer; reload before saving")

// Update reads and changes the latest record under the process-wide file
// lock. Callbacks must only change this record and must not perform Git I/O.
func (db *Database) Update(change func(*Record) error) (Record, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	release, err := lockRecord(db.path)
	if err != nil {
		return Record{}, err
	}
	defer release()
	data, err := os.ReadFile(db.path)
	record := Record{}
	if errors.Is(err, os.ErrNotExist) {
		// Preserve the scope of a freshly opened database.
		record.Scope = db.scope
		record.Viewed = map[string]string{}
	} else if err != nil {
		return record, err
	} else if err = json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if err = change(&record); err != nil {
		return record, err
	}
	record.Sequence++
	if err = writeJSON(db.path, record); err != nil {
		return record, err
	}
	db.sequence = record.Sequence
	db.base = record.Clone()
	return record, nil
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

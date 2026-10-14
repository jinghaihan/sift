package store

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"slices"
	"strings"
)

const maxReviewBytes = 32 << 20

// ReviewFile is a portable review. Local repository paths and save sequencing
// belong to Record and are deliberately not part of this format.
type ReviewFile struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	Repository ReviewRepository `json:"repository"`
	Scope      ReviewScope      `json:"scope"`
	Revision   string           `json:"revision"`
	Comments   []Comment        `json:"comments"`
}

type ReviewRepository struct {
	Name   string `json:"name"`
	Remote string `json:"remote,omitempty"`
}

type ReviewScope struct {
	Branch      string `json:"branch"`
	Integration string `json:"integration"`
	Start       string `json:"start"`
	Historical  string `json:"historical,omitempty"`
}

func ExportReview(record Record, name, remote string) ReviewFile {
	branch, historical, _ := strings.Cut(record.Scope.Branch, "::historical:")
	return ReviewFile{
		Format: "sift-review", Version: 1,
		Repository: ReviewRepository{Name: name, Remote: RepositoryRemote(remote)},
		Scope:      ReviewScope{Branch: branch, Integration: record.Scope.Integration, Start: record.Scope.Start, Historical: historical},
		Revision:   record.Tip, Comments: slices.Clone(record.Comments),
	}
}

// RepositoryRemote strips URL credentials, query tokens and fragments. An
// origin pointing at a local directory is not portable repository metadata.
func RepositoryRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return strings.TrimSuffix(u.String(), ".git")
	}
	if _, rest, ok := strings.Cut(remote, "@"); ok {
		if host, path, found := strings.Cut(rest, ":"); found && host != "" && path != "" {
			return "ssh://" + host + "/" + strings.TrimSuffix(path, ".git")
		}
	}
	return ""
}

func ReadReview(r io.Reader) (ReviewFile, error) {
	var file ReviewFile
	data, err := io.ReadAll(io.LimitReader(r, maxReviewBytes+1))
	if err != nil {
		return file, err
	}
	if len(data) > maxReviewBytes {
		return file, fmt.Errorf("review file exceeds 32 MB")
	}
	if err = json.Unmarshal(data, &file); err != nil {
		return file, fmt.Errorf("read review file: %w", err)
	}
	return file, file.Validate()
}

func (file ReviewFile) Validate() error {
	if file.Format != "sift-review" || file.Version != 1 {
		return fmt.Errorf("unsupported review file format or version")
	}
	if strings.TrimSpace(file.Scope.Branch) == "" || strings.TrimSpace(file.Scope.Integration) == "" || !commitID(file.Scope.Start) || !commitID(file.Revision) || file.Scope.Historical != "" && !commitID(file.Scope.Historical) {
		return fmt.Errorf("review file has an invalid branch, range or revision")
	}
	if len(file.Comments) > 10000 {
		return fmt.Errorf("review file contains too many comments")
	}
	ids := map[string]bool{}
	for _, cm := range file.Comments {
		a := cm.Anchor
		if cm.ID == "" || len(cm.ID) > 128 || ids[cm.ID] || !fs.ValidPath(cm.Path) || cm.Path == "." || strings.ContainsAny(cm.Path, "\\\x00") || strings.TrimSpace(cm.Body) == "" {
			return fmt.Errorf("review file contains an invalid or duplicate comment")
		}
		if a.Side != 0 && a.Side != 1 || a.Start < 1 || a.End < a.Start || a.End > 100000000 || a.End-a.Start >= 10000 || len(strings.Split(a.Excerpt, "\n")) != a.End-a.Start+1 {
			return fmt.Errorf("review file contains an invalid comment location")
		}
		ids[cm.ID] = true
	}
	return nil
}

func commitID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func WriteReview(path string, file ReviewFile) error {
	if err := file.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxReviewBytes {
		return fmt.Errorf("review file exceeds 32 MB")
	}
	return writeJSON(path, file)
}

type CommentConflict struct {
	Local    Comment
	Imported Comment
}

func CommentConflicts(local, imported []Comment) []CommentConflict {
	byID := map[string]Comment{}
	for _, cm := range local {
		byID[cm.ID] = cm
	}
	var conflicts []CommentConflict
	for _, incoming := range imported {
		if cm, ok := byID[incoming.ID]; ok && !sameComment(cm, incoming) {
			conflicts = append(conflicts, CommentConflict{Local: cm, Imported: incoming})
		}
	}
	return conflicts
}

func sameComment(a, b Comment) bool {
	// Line numbers and outdated status can change when an anchor relocates.
	return a.Path == b.Path && a.Body == b.Body && a.Resolved == b.Resolved && a.Anchor.Side == b.Anchor.Side && a.Anchor.Excerpt == b.Anchor.Excerpt && a.Anchor.Before == b.Anchor.Before && a.Anchor.After == b.Anchor.After
}

// MergeComments adds new IDs and only replaces an existing comment when the
// user explicitly chose its imported copy. Identical imports are idempotent.
func MergeComments(local, imported []Comment, replace map[string]bool) []Comment {
	merged := slices.Clone(local)
	index := map[string]int{}
	for i, cm := range merged {
		index[cm.ID] = i
	}
	for _, incoming := range imported {
		if i, ok := index[incoming.ID]; ok {
			if replace[incoming.ID] {
				merged[i] = incoming
			}
		} else {
			index[incoming.ID] = len(merged)
			merged = append(merged, incoming)
		}
	}
	return merged
}

package workflow

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/jinghaihan/sift/internal/store"
)

type EditHeader struct {
	Version       int     `json:"version"`
	Revision      string  `json:"revision"`
	ReviewVersion *uint64 `json:"reviewVersion"`
}

type Draft struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Side  string `json:"side"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Body  string `json:"body"`
}

type DraftBatch struct {
	EditHeader
	Owner    string  `json:"owner"`
	Comments []Draft `json:"comments"`
}

func (s *Session) checkEdit(header EditHeader) error {
	if header.Version != 1 {
		return fmt.Errorf("unsupported edit format version")
	}
	if header.Revision != s.Result.Tip {
		return fmt.Errorf("stale revision: analyze the current branch before editing")
	}
	if header.ReviewVersion == nil || *header.ReviewVersion != s.Record.Sequence {
		return store.ErrConcurrentWrite
	}
	return nil
}

func (s *Session) commit(header EditHeader, next store.Record, dry bool) (store.Record, error) {
	if err := s.checkEdit(header); err != nil {
		return store.Record{}, err
	}
	next.Tip, next.Provenance = s.Result.Tip, s.Result.Provenance
	if dry {
		next.Sequence = s.Record.Sequence + 1
		return next, nil
	}
	record, err := s.Database.Update(func(current *store.Record) error {
		if current.Sequence != *header.ReviewVersion || current.Tip != s.Record.Tip {
			return store.ErrConcurrentWrite
		}
		sequence := current.Sequence
		*current = next.Clone()
		current.Sequence = sequence
		return nil
	})
	if err == nil {
		err = store.SaveScope(s.DataDir, s.Result.Scope)
	}
	return record, err
}

func ownerName(value string) (string, error) {
	if value == "" {
		value = "agent"
	}
	if len(value) > 40 {
		return "", fmt.Errorf("owner is too long")
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", fmt.Errorf("owner must contain only letters, digits, underscores or hyphens")
		}
	}
	return "agent:" + value, nil
}

func (s *Session) nextRecord(ctx context.Context) store.Record {
	next := s.Record.Clone()
	if next.Tip != "" && next.Tip != s.Result.Tip {
		for i, cm := range next.Comments {
			location := s.locate(ctx, store.ScriptReference{ID: cm.ID, Kind: "code", Path: cm.Path, Anchor: cm.Anchor, Revision: next.Tip}, nil)
			if location.Status == "unresolved" {
				next.Comments[i].Outdated = true
			} else {
				next.Comments[i].Anchor = location.Anchor
				next.Comments[i].Outdated = false
			}
		}
	}
	return next
}

func (s *Session) UpsertDrafts(ctx context.Context, batch DraftBatch, dry bool) (store.Record, error) {
	if err := s.checkEdit(batch.EditHeader); err != nil {
		return store.Record{}, err
	}
	owner, err := ownerName(batch.Owner)
	if err != nil {
		return store.Record{}, err
	}
	next := s.nextRecord(ctx)
	seen := map[string]bool{}
	for _, draft := range batch.Comments {
		if draft.ID == "" || len(draft.ID) > 128 || seen[draft.ID] || strings.TrimSpace(draft.Body) == "" {
			return next, fmt.Errorf("draft has an invalid or duplicate ID or empty body")
		}
		seen[draft.ID] = true
		side := 1
		if draft.Side == "old" {
			side = 0
		} else if draft.Side != "new" {
			return next, fmt.Errorf("draft side must be old or new")
		}
		end := draft.End
		if end == 0 {
			end = draft.Start
		}
		anchor, err := s.Anchor(ctx, draft.Path, side, draft.Start, end, "")
		if err != nil {
			return next, err
		}
		id := owner + ":" + draft.ID
		if strings.HasPrefix(draft.ID, owner+":") {
			id = draft.ID
		}
		if len(id) > 128 {
			return next, fmt.Errorf("draft ID is too long")
		}
		at := slices.IndexFunc(next.Comments, func(c store.Comment) bool { return c.ID == id })
		comment := store.NewComment("feature:"+draft.Path, draft.Path, draft.Body, anchor)
		comment.ID, comment.Source = id, owner
		if at < 0 {
			next.Comments = append(next.Comments, comment)
		} else {
			if next.Comments[at].Source != owner {
				return next, fmt.Errorf("draft ID belongs to another writer")
			}
			comment.Created = next.Comments[at].Created
			comment.Resolved = next.Comments[at].Resolved
			next.Comments[at] = comment
		}
	}
	return s.commit(batch.EditHeader, next, dry)
}

type ReferenceEdit struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind,omitempty"`
	CommentID  string  `json:"commentId,omitempty"`
	Path       string  `json:"path,omitempty"`
	Side       string  `json:"side,omitempty"`
	Start      int     `json:"start,omitempty"`
	End        int     `json:"end,omitempty"`
	Historical *string `json:"historical,omitempty"`
}

type SectionEdit struct {
	ID               string          `json:"id"`
	Title            *string         `json:"title,omitempty"`
	Body             *string         `json:"body,omitempty"`
	References       []ReferenceEdit `json:"references,omitempty"`
	DeleteReferences []string        `json:"deleteReferences,omitempty"`
}

type ScriptBatch struct {
	EditHeader
	Owner    string        `json:"owner,omitempty"`
	Sections []SectionEdit `json:"sections"`
	Delete   []string      `json:"delete,omitempty"`
	Order    []string      `json:"order,omitempty"`
}

func (s *Session) Reference(ctx context.Context, input ReferenceEdit, previous store.ScriptReference) (store.ScriptReference, error) {
	ref := previous
	ref.ID = input.ID
	if ref.ID == "" {
		return ref, fmt.Errorf("reference ID is required")
	}
	if input.Kind != "" {
		ref.Kind = input.Kind
	}
	if input.CommentID != "" {
		ref.CommentID = input.CommentID
	}
	if input.Path != "" {
		ref.Path = input.Path
	}
	if input.Historical != nil {
		ref.Historical = *input.Historical
	}
	if ref.Kind == "comment" {
		comments := s.Record.Comments
		if ref.Historical != "" {
			scope := s.Result.Scope
			scope.Branch += "::historical:" + ref.Historical
			_, record, err := store.Open(s.DataDir, scope)
			if err != nil {
				return ref, err
			}
			comments = record.Comments
		}
		at := slices.IndexFunc(comments, func(c store.Comment) bool { return c.ID == ref.CommentID })
		if at < 0 {
			return ref, fmt.Errorf("linked comment does not exist")
		}
		ref.Path, ref.Anchor = comments[at].Path, comments[at].Anchor
	} else if ref.Kind == "code" {
		side := ref.Anchor.Side
		if input.Side == "new" {
			side = 1
		} else if input.Side == "old" {
			side = 0
		} else if input.Side != "" {
			return ref, fmt.Errorf("reference side must be old or new")
		}
		if previous.ID == "" && input.Side == "" {
			side = 1
		}
		start, end := input.Start, input.End
		if start == 0 {
			located := s.locate(ctx, ref, nil)
			if located.Status == "unresolved" {
				return ref, fmt.Errorf("reference needs a new code location")
			}
			start, end = located.Anchor.Start, located.Anchor.End
		}
		if end == 0 {
			end = start
		}
		anchor, err := s.Anchor(ctx, ref.Path, side, start, end, ref.Historical)
		if err != nil {
			return ref, err
		}
		ref.Anchor = anchor
	} else {
		return ref, fmt.Errorf("reference kind must be code or comment")
	}
	ref.Revision = s.Result.Tip
	if ref.Historical != "" {
		ref.Revision = ref.Historical
	}
	return ref, nil
}

func (s *Session) UpsertScript(ctx context.Context, batch ScriptBatch, dry bool) (store.Record, error) {
	if err := s.checkEdit(batch.EditHeader); err != nil {
		return store.Record{}, err
	}
	owner, err := ownerName(batch.Owner)
	if err != nil {
		return store.Record{}, err
	}
	next := s.nextRecord(ctx)
	seen := map[string]bool{}
	for _, edit := range batch.Sections {
		if edit.ID == "" || seen[edit.ID] {
			return next, fmt.Errorf("section ID is missing or duplicated")
		}
		seen[edit.ID] = true
		at := slices.IndexFunc(next.Script, func(section store.ScriptSection) bool { return section.ID == edit.ID })
		if at < 0 {
			next.Script = append(next.Script, store.ScriptSection{ID: edit.ID, Source: owner})
			at = len(next.Script) - 1
		}
		section := &next.Script[at]
		if edit.Title != nil {
			section.Title = *edit.Title
		}
		if edit.Body != nil {
			section.Body = *edit.Body
		}
		refIDs := map[string]bool{}
		for _, input := range edit.References {
			if refIDs[input.ID] {
				return next, fmt.Errorf("reference ID is duplicated")
			}
			refIDs[input.ID] = true
			index := slices.IndexFunc(section.References, func(ref store.ScriptReference) bool { return ref.ID == input.ID })
			old := store.ScriptReference{}
			if index >= 0 {
				old = section.References[index]
			}
			ref, err := s.Reference(ctx, input, old)
			if err != nil {
				return next, err
			}
			if index < 0 {
				section.References = append(section.References, ref)
			} else {
				section.References[index] = ref
			}
		}
		section.References = slices.DeleteFunc(section.References, func(ref store.ScriptReference) bool { return slices.Contains(edit.DeleteReferences, ref.ID) })
	}
	next.Script = slices.DeleteFunc(next.Script, func(section store.ScriptSection) bool { return slices.Contains(batch.Delete, section.ID) })
	if len(batch.Order) > 0 {
		ordered := []store.ScriptSection{}
		used := map[string]bool{}
		for _, id := range batch.Order {
			at := slices.IndexFunc(next.Script, func(s store.ScriptSection) bool { return s.ID == id })
			if at < 0 || used[id] {
				return next, fmt.Errorf("order contains an unknown or duplicate section")
			}
			ordered = append(ordered, next.Script[at])
			used[id] = true
		}
		for _, section := range next.Script {
			if !used[section.ID] {
				ordered = append(ordered, section)
			}
		}
		next.Script = ordered
	}
	if err := store.ValidateScript(next.Script); err != nil {
		return next, err
	}
	return s.commit(batch.EditHeader, next, dry)
}

func (s *Session) Import(ctx context.Context, file store.ReviewFile, dry, allowStale bool, policy string) (store.Record, error) {
	if err := file.Validate(); err != nil {
		return store.Record{}, err
	}
	if file.Scope.Branch != s.Result.Branch {
		return store.Record{}, fmt.Errorf("bundle belongs to another branch")
	}
	next := s.nextRecord(ctx)
	replace := map[string]bool{}
	for _, conflict := range store.CommentConflicts(next.Comments, file.Comments) {
		if policy == "incoming" {
			replace[conflict.Imported.ID] = true
		} else if policy != "keep-local" {
			return next, fmt.Errorf("comment %s conflicts with a local draft", conflict.Imported.ID)
		}
	}
	comments := slices.Clone(file.Comments)
	for i := range comments {
		ref := store.ScriptReference{ID: comments[i].ID, Kind: "code", Path: comments[i].Path, Anchor: comments[i].Anchor, Revision: file.Revision}
		located := s.locate(ctx, ref, nil)
		if located.Status == "unresolved" {
			if !allowStale {
				return next, fmt.Errorf("comment %s cannot be located: %s", ref.ID, located.Message)
			}
			comments[i].Outdated = true
		} else {
			comments[i].Anchor = located.Anchor
		}
	}
	next.Comments = store.MergeComments(next.Comments, comments, replace)
	for _, section := range file.Script {
		for _, ref := range section.References {
			location := s.locate(ctx, ref, next.Comments)
			if location.Status != "valid" && !allowStale {
				return next, fmt.Errorf("reference %s is %s; use --allow-stale to retain it", ref.ID, location.Status)
			}
		}
		at := slices.IndexFunc(next.Script, func(s store.ScriptSection) bool { return s.ID == section.ID })
		if at < 0 {
			next.Script = append(next.Script, section)
		} else {
			if !sameSection(next.Script[at], section) && policy != "incoming" && policy != "keep-local" {
				return next, fmt.Errorf("script section %s conflicts with a local section", section.ID)
			}
			if policy == "incoming" {
				next.Script[at] = section
			}
		}
	}
	version := s.Record.Sequence
	return s.commit(EditHeader{Version: 1, Revision: s.Result.Tip, ReviewVersion: &version}, next, dry)
}

func sameSection(a, b store.ScriptSection) bool { return reflect.DeepEqual(a, b) }

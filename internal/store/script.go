package store

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

type ScriptSection struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	References []ScriptReference `json:"references,omitempty"`
	Source     string            `json:"source,omitempty"`
}

type ScriptReference struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	CommentID  string `json:"commentId,omitempty"`
	Path       string `json:"path,omitempty"`
	Anchor     Anchor `json:"anchor"`
	Revision   string `json:"revision"`
	Historical string `json:"historical,omitempty"`
}

func CloneScript(sections []ScriptSection) []ScriptSection {
	result := slices.Clone(sections)
	for i := range result {
		result[i].References = slices.Clone(result[i].References)
	}
	return result
}

func ValidateScript(sections []ScriptSection) error {
	if len(sections) > 1000 {
		return fmt.Errorf("script contains too many sections")
	}
	ids := map[string]bool{}
	for _, section := range sections {
		if section.ID == "" || len(section.ID) > 128 || ids[section.ID] || strings.ContainsAny(section.Title, "\r\n") || len(section.Body) > 2<<20 {
			return fmt.Errorf("script contains an invalid or duplicate section")
		}
		ids[section.ID] = true
		if len(section.References) > 1000 {
			return fmt.Errorf("section contains too many references")
		}
		refs := map[string]bool{}
		for _, ref := range section.References {
			if ref.ID == "" || len(ref.ID) > 128 || refs[ref.ID] || !commitID(ref.Revision) || ref.Historical != "" && !commitID(ref.Historical) {
				return fmt.Errorf("script contains an invalid reference")
			}
			refs[ref.ID] = true
			if ref.Path != "" && (!fs.ValidPath(ref.Path) || ref.Path == "." || strings.ContainsAny(ref.Path, "\\\x00")) {
				return fmt.Errorf("reference contains an invalid path")
			}
			if ref.Kind == "comment" {
				if ref.CommentID == "" {
					return fmt.Errorf("comment reference is missing its comment ID")
				}
			} else if ref.Kind == "code" {
				if !fs.ValidPath(ref.Path) || ref.Path == "." || strings.ContainsAny(ref.Path, "\\\x00") || !validAnchor(ref.Anchor) {
					return fmt.Errorf("code reference has an invalid path or anchor")
				}
			} else {
				return fmt.Errorf("unknown reference kind %q", ref.Kind)
			}
		}
	}
	return nil
}

func validAnchor(a Anchor) bool {
	return (a.Side == 0 || a.Side == 1) && a.Start > 0 && a.End >= a.Start && a.End <= 100000000 && a.End-a.Start < 10000 && len(strings.Split(a.Excerpt, "\n")) == a.End-a.Start+1
}

func ScriptMarkdown(record Record) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Review script\n\nBranch: `%s`\n\nRevision: `%s`\n", record.Scope.Branch, record.Tip)
	for _, section := range record.Script {
		fmt.Fprintf(&out, "\n## %s\n\n%s\n", section.Title, section.Body)
		for _, ref := range section.References {
			side := "new"
			if ref.Anchor.Side == 0 {
				side = "old"
			}
			location := fmt.Sprintf("`%s:%d–%d` (%s)", ref.Path, ref.Anchor.Start, ref.Anchor.End, side)
			if ref.Kind == "comment" {
				if ref.Path == "" {
					location = "Comment `" + ref.CommentID + "`"
				} else {
					location += " · comment `" + ref.CommentID + "`"
				}
			}
			if ref.Historical != "" {
				location += " · historical patch `" + ref.Historical + "`"
			}
			fmt.Fprintf(&out, "\n- %s · revision `%s`\n", location, ref.Revision)
		}
	}
	return out.String()
}

func mergeScript(base, incoming, latest []ScriptSection) ([]ScriptSection, error) {
	merged, err := mergeItems(base, incoming, latest, func(s ScriptSection) string { return s.ID })
	if err != nil {
		return nil, err
	}
	baseIDs := map[string]bool{}
	for _, s := range base {
		baseIDs[s.ID] = true
	}
	order := func(sections []ScriptSection) []string {
		var result []string
		for _, s := range sections {
			if baseIDs[s.ID] {
				result = append(result, s.ID)
			}
		}
		return result
	}
	before, wanted, current := order(base), order(incoming), order(latest)
	if slices.Equal(before, wanted) {
		return merged, nil
	}
	// Deletions alone do not claim ownership of the ordering of foreign items.
	if len(wanted) != len(before) {
		return merged, nil
	}
	if !slices.Equal(before, current) && !slices.Equal(wanted, current) {
		return nil, ErrConcurrentWrite
	}
	byID := map[string]ScriptSection{}
	for _, s := range merged {
		byID[s.ID] = s
	}
	result := []ScriptSection{}
	for _, id := range wanted {
		if s, ok := byID[id]; ok {
			result = append(result, s)
			delete(byID, id)
		}
	}
	for _, s := range merged {
		if _, ok := byID[s.ID]; ok {
			result = append(result, s)
		}
	}
	return result, nil
}

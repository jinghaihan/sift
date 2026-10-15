package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jinghaihan/sift/internal/review"
)

func TestPortableScriptPreservesOrderReferencesAndProvenance(t *testing.T) {
	file := portableReview()
	file.Provenance = []review.CommitProvenance{{ID: file.Revision, Role: "integration_update", Previous: file.Scope.Start}, {ID: file.Scope.Start, Role: "feature"}}
	file.Script = []ScriptSection{
		{ID: "two", Title: "第二部分", Body: "## Details\n\n**Why** this matters.", References: []ScriptReference{{ID: "ref", Kind: "code", Path: "x.go", Anchor: file.Comments[0].Anchor, Revision: file.Revision}}},
		{ID: "one", Title: "第一部分", References: []ScriptReference{{ID: "note", Kind: "comment", CommentID: file.Comments[0].ID, Path: "x.go", Anchor: file.Comments[0].Anchor, Revision: file.Revision}}},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadReview(bytes.NewReader(raw))
	if err != nil || !reflect.DeepEqual(got, file) {
		t.Fatalf("portable script round trip: %+v %v", got, err)
	}
	clone := Record{Script: file.Script, Provenance: file.Provenance}.Clone()
	clone.Script[0].References[0].Path = "other.go"
	clone.Provenance[0].Previous = "changed"
	if file.Script[0].References[0].Path != "x.go" || file.Provenance[0].Previous != file.Scope.Start {
		t.Fatal("snapshot shares reference or provenance memory")
	}
	file.Version, file.Script, file.Provenance = 1, nil, nil
	raw, _ = json.Marshal(file)
	if _, err = ReadReview(bytes.NewReader(raw)); err != nil {
		t.Fatalf("old v1 bundle is no longer readable: %v", err)
	}
}

func TestScriptRejectsInvalidAndDuplicateReferences(t *testing.T) {
	valid := []ScriptSection{{ID: "section", References: []ScriptReference{{ID: "ref", Kind: "code", Path: "a.go", Revision: strings.Repeat("a", 40), Anchor: Anchor{Side: 1, Start: 1, End: 1, Excerpt: "context"}}}}}
	for name, mutate := range map[string]func([]ScriptSection){
		"absolute code path": func(s []ScriptSection) { s[0].References[0].Path = "/private/code.go" },
		"absolute comment path": func(s []ScriptSection) {
			s[0].References[0].Kind = "comment"
			s[0].References[0].CommentID = "note"
			s[0].References[0].Path = "/private/code.go"
		},
		"duplicate reference": func(s []ScriptSection) { s[0].References = append(s[0].References, s[0].References[0]) },
		"invalid revision":    func(s []ScriptSection) { s[0].References[0].Revision = "HEAD" },
		"invalid anchor":      func(s []ScriptSection) { s[0].References[0].Anchor.Start = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			s := CloneScript(valid)
			mutate(s)
			if err := ValidateScript(s); err == nil {
				t.Fatal("accepted invalid script")
			}
		})
	}
}

func TestConcurrentScriptOrderingAndForeignAdditions(t *testing.T) {
	base := []ScriptSection{{ID: "one", Title: "one"}, {ID: "two", Title: "two"}}
	latest := append(CloneScript(base), ScriptSection{ID: "foreign", Body: "Another writer's section."})
	merged, err := mergeScript(base, []ScriptSection{base[1], base[0]}, latest)
	if err != nil || !slices.EqualFunc(merged, []ScriptSection{base[1], base[0], latest[2]}, sameItem[ScriptSection]) {
		t.Fatalf("reorder lost another writer's section: %+v %v", merged, err)
	}
	changed := CloneScript(base)
	changed[0].Body = "GUI changed body."
	latest[0].Body = "CLI changed body."
	if _, err := mergeScript(base, changed, latest); !errors.Is(err, ErrConcurrentWrite) {
		t.Fatalf("conflicting manual edit was overwritten: %v", err)
	}
}

package review

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var integrationName = regexp.MustCompile(`^(main|master|develop|development|dev|dev[0-9]+|release[/-].+)$`)

type candidate struct {
	name      string
	mainline  map[string]bool
	positions map[string]int
}

// DetectScope uses integration mainlines, rather than a merge-base that may
// already contain this feature. The nearest recorded incoming merge identifies
// which integration branch supplied its background. No author filter is used.
func DetectScope(ctx context.Context, path, branch string) (Scope, error) {
	scope := Scope{Repository: path, Branch: branch}
	r, err := openRepository(ctx, path)
	if err != nil {
		return scope, err
	}
	scope.Repository = r.path
	tip, err := r.resolve(ctx, branch)
	if err != nil {
		return scope, err
	}
	refs, err := r.run(ctx, "for-each-ref", "--format=%(refname:short)", "refs/heads/", "refs/remotes/")
	if err != nil {
		return scope, err
	}
	var candidates []candidate
	for name := range strings.FieldsSeq(refs) {
		short := name
		if strings.HasPrefix(name, "origin/") {
			short = strings.TrimPrefix(name, "origin/")
		}
		if name == branch || !integrationName.MatchString(short) {
			continue
		}
		output, err := r.run(ctx, "rev-list", "--first-parent", name, "--")
		if err != nil {
			return scope, err
		}
		c := candidate{name: name, mainline: map[string]bool{}, positions: map[string]int{}}
		for i, id := range strings.Fields(output) {
			c.mainline[id] = true
			c.positions[id] = i
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return scope, fmt.Errorf("no integration branch detected; choose the review range")
	}
	history, err := History(ctx, r.path, tip)
	if err != nil {
		return scope, err
	}
	byID := map[string]Commit{}
	for _, commit := range history {
		byID[commit.ID] = commit
	}
	// Find the most recent incoming merge whose second parent is on a known
	// integration mainline. When refs overlap, prefer the closest ref tip.
	chosen := -1
	for commit := byID[tip]; commit.ID != ""; {
		backgroundCommit := false
		for _, c := range candidates {
			backgroundCommit = backgroundCommit || c.mainline[commit.ID]
		}
		if len(commit.Parents) > 1 && !backgroundCommit {
			best := int(^uint(0) >> 1)
			for i, c := range candidates {
				if distance, ok := c.positions[commit.Parents[1]]; ok && distance < best {
					chosen, best = i, distance
				}
			}
			if chosen >= 0 {
				break
			}
		}
		if len(commit.Parents) == 0 {
			break
		}
		commit = byID[commit.Parents[0]]
	}
	if chosen < 0 {
		// Without incoming merges, the nearest shared mainline identifies a
		// simple feature branch's fork. Equal refs are equivalent scopes.
		for commit := byID[tip]; commit.ID != ""; {
			for i, c := range candidates {
				if c.mainline[commit.ID] {
					chosen = i
					break
				}
			}
			if chosen >= 0 {
				break
			}
			if len(commit.Parents) == 0 {
				break
			}
			commit = byID[commit.Parents[0]]
		}
	}
	if chosen < 0 {
		return scope, fmt.Errorf("cannot infer a shared integration mainline; choose the review range")
	}
	background := candidates[chosen]
	scope.Integration = background.name
	// A local creation reflog gives the original fork even after a merged
	// integration branch was fast-forwarded back. Ignore tracking-clone
	// creation entries whose tip is not on the integration mainline.
	if output, logErr := r.run(ctx, "reflog", "show", "--format=%H%x00%gs", branch, "--"); logErr == nil {
		for line := range strings.SplitSeq(output, "\n") {
			base, message, ok := strings.Cut(line, "\x00")
			if !ok || !strings.HasPrefix(message, "branch: Created from ") || !background.mainline[base] {
				continue
			}
			if base == tip {
				return scope, ErrNoFeatureChanges
			}
			path, pathErr := featureHistory(history, base, background.mainline)
			if pathErr != nil {
				continue
			}
			for i := len(path) - 2; i >= 0; i-- {
				if !background.mainline[path[i].ID] {
					scope.Start = path[i].ID
					return scope, nil
				}
			}
		}
	}
	var first string
	for commit := byID[tip]; commit.ID != ""; {
		if background.mainline[commit.ID] {
			if first == "" {
				return scope, fmt.Errorf("this branch is on the integration mainline; choose the review range")
			}
			if strings.Contains(commit.Subject, "'"+branch+"'") || strings.HasSuffix(commit.Subject, " "+branch) {
				return scope, fmt.Errorf("feature history was fast-forwarded through integration; choose its first commit")
			}
			scope.Start = first
			return scope, nil
		}
		first = commit.ID
		if len(commit.Parents) == 0 {
			break
		}
		commit = byID[commit.Parents[0]]
	}
	return scope, fmt.Errorf("cannot infer the first feature commit; choose the review range")
}

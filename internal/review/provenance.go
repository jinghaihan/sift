package review

import (
	"context"
	"strings"
)

const (
	featureCommit     = "feature"
	integrationMerge  = "integration_merge"
	featureMerge      = "feature_merge"
	integrationUpdate = "integration_update"
)

// CommitProvenance records a commit's role when the feature was reviewed. A
// later fast-forward into the integration branch does not change that role.
type CommitProvenance struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Previous string `json:"previous,omitempty"`
}

type branchEvidence struct {
	base     string
	fork     bool
	roles    map[string]string
	previous map[string]string
}

// recordedFeature reads operations on this branch, not the author's commits
// elsewhere in the repository. Fetches, resets and fast-forwards create no
// feature commits. Ignore obsolete/amended commits outside the current graph.
func recordedFeature(ctx context.Context, r gitRepository, branch, integration string, history []Commit) branchEvidence {
	evidence := branchEvidence{roles: map[string]string{}, previous: map[string]string{}}
	byID := map[string]Commit{}
	for _, commit := range history {
		byID[commit.ID] = commit
	}
	refs := []string{branch}
	full, err := r.run(ctx, "rev-parse", "--symbolic-full-name", "--verify", "--end-of-options", branch)
	if err != nil {
		return evidence
	}
	// Remote-tracking refs have fetch records. A matching local upstream can
	// supply the original development operations without changing the checkout.
	if strings.HasPrefix(strings.TrimSpace(full), "refs/remotes/") {
		output, _ := r.run(ctx, "for-each-ref", "--format=%(refname:short)%00%(upstream)", "refs/heads/")
		for line := range strings.SplitSeq(output, "\n") {
			name, upstream, ok := strings.Cut(line, "\x00")
			if ok && upstream == strings.TrimSpace(full) {
				refs = append(refs, name)
			}
		}
	}
	for _, ref := range refs {
		output, err := r.run(ctx, "reflog", "show", "--format=%H%x00%gs", ref, "--")
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for i, line := range lines {
			id, operation, ok := strings.Cut(line, "\x00")
			commit, reachable := byID[id]
			if !ok || !reachable {
				continue
			}
			switch {
			case strings.HasPrefix(operation, "branch: Created from "):
				if evidence.base == "" {
					evidence.base = id
					source := strings.TrimPrefix(operation, "branch: Created from ")
					evidence.fork = source == "HEAD" || isIntegration(source, integration)
				}
			case strings.HasPrefix(operation, "commit:") || strings.HasPrefix(operation, "commit (") || strings.HasPrefix(operation, "cherry-pick:") || strings.HasPrefix(operation, "revert:"):
				if len(commit.Parents) == 1 {
					evidence.roles[id] = featureCommit
				} else if len(commit.Parents) > 1 {
					evidence.roles[id] = integrationMerge
				}
			case strings.HasPrefix(operation, "merge ") && !strings.Contains(operation, "Fast-forward"):
				if len(commit.Parents) < 2 {
					continue
				}
				name, _, _ := strings.Cut(strings.TrimPrefix(operation, "merge "), ": ")
				role := featureMerge
				if isIntegration(name, integration) {
					role = integrationMerge
				}
				evidence.roles[id] = role
			case strings.HasPrefix(operation, "merge ") && strings.Contains(operation, "Fast-forward"):
				name, _, _ := strings.Cut(strings.TrimPrefix(operation, "merge "), ": ")
				if i+1 < len(lines) && isIntegration(name, integration) {
					previous, _, _ := strings.Cut(lines[i+1], "\x00")
					if _, exists := byID[previous]; exists && previous != id && evidence.previous[id] == "" {
						evidence.previous[id] = previous
					}
				}
			}
		}
	}
	return evidence
}

func isIntegration(name, integration string) bool {
	return name == integration || strings.TrimPrefix(name, "origin/") == strings.TrimPrefix(integration, "origin/") || integrationName.MatchString(strings.TrimPrefix(name, "origin/"))
}

func applyFeatureEvidence(imported map[string]bool, roles map[string]string) {
	for id := range roles {
		delete(imported, id)
	}
}

package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidScope = errors.New("invalid feature history")
var ErrNoFeatureChanges = errors.New("the branch has no commits beyond its recorded starting point")

type Scope struct {
	Repository  string `json:"repository"`
	Branch      string `json:"branch"`
	Integration string `json:"integration"`
	Start       string `json:"start"`
}

type Options struct {
	Scope
	KnownCommits []CommitProvenance
	OnProgress   func(string)
	KeepSnapshot bool
}

// Uncertain is a historical patch that could not be isolated safely. It is
// separate from Patch, which is the net change of successfully isolated work.
type Uncertain struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"`
	Patch   string `json:"patch"`
}

type Result struct {
	Scope
	Provenance   []CommitProvenance `json:"provenance,omitempty"`
	Tip          string             `json:"tip"`
	Commits      int                `json:"commits"`
	Merges       int                `json:"merges"`
	Imported     int                `json:"imported"`
	Patch        string             `json:"patch"`
	Uncertain    []Uncertain        `json:"uncertain,omitempty"`
	SnapshotPath string             `json:"-"`
	Baseline     string             `json:"-"`
}

// Close releases a retained comparison snapshot after its viewer closes.
func (r Result) Close() error {
	if r.SnapshotPath == "" {
		return nil
	}
	return os.RemoveAll(filepath.Dir(r.SnapshotPath))
}

// Analyze constructs a counterfactual tree by reversing the feature's own
// first-parent commits from its tip. Three-way reverts preserve upstream work
// brought in through merges. All mutations are confined to a temporary clone.
func Analyze(ctx context.Context, options Options) (Result, error) {
	result := Result{Scope: options.Scope}
	r, err := openRepository(ctx, options.Repository)
	if err != nil {
		return result, err
	}
	result.Repository = r.path
	result.Tip, err = r.resolve(ctx, options.Branch)
	if err != nil {
		return result, err
	}
	start, err := r.resolve(ctx, options.Start)
	if err != nil {
		return result, fmt.Errorf("%w: select the first feature commit: %v", ErrInvalidScope, err)
	}
	result.Start = start
	background, err := r.resolve(ctx, options.Integration)
	if err != nil {
		return result, fmt.Errorf("%w: select the integration branch: %v", ErrInvalidScope, err)
	}
	backgroundHistory, err := r.run(ctx, "rev-list", "--first-parent", background, "--")
	if err != nil {
		return result, err
	}
	imported := map[string]bool{}
	for id := range strings.FieldsSeq(backgroundHistory) {
		imported[id] = true
	}
	history, err := History(ctx, r.path, result.Tip)
	if err != nil {
		return result, err
	}
	evidence := recordedFeature(ctx, r, options.Branch, options.Integration, history)
	reachable := map[string]bool{}
	for _, commit := range history {
		reachable[commit.ID] = true
	}
	for _, known := range options.KnownCommits {
		if known.Role == integrationUpdate && reachable[known.ID] && reachable[known.Previous] && evidence.previous[known.ID] == "" {
			evidence.previous[known.ID] = known.Previous
		}
		if reachable[known.ID] && (known.Role == featureCommit || known.Role == integrationMerge || known.Role == featureMerge) {
			if evidence.roles[known.ID] == "" {
				evidence.roles[known.ID] = known.Role
			}
		}
	}
	applyFeatureEvidence(imported, evidence.roles)
	if imported[start] {
		return result, fmt.Errorf("%w: the first feature commit is already on the integration mainline and its branch provenance is unavailable", ErrInvalidScope)
	}
	history, err = featureHistory(history, start, imported, evidence.previous)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidScope, err)
	}
	if len(history[len(history)-1].Parents) == 0 {
		return result, fmt.Errorf("%w: select a feature commit after the repository's initial commit", ErrInvalidScope)
	}

	temporary, err := os.MkdirTemp("", "sift-review-")
	if err != nil {
		return result, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(temporary)
		}
	}()
	work := gitRepository{path: filepath.Join(temporary, "work")}
	progress := func(message string) {
		if options.OnProgress != nil {
			options.OnProgress(message)
		}
	}
	progress("Preparing review snapshot…")
	if _, err = r.run(ctx, "clone", "--quiet", "--shared", "--no-checkout", "--", r.path, work.path); err != nil {
		return result, err
	}
	// A review must not execute a user's checkout hooks or reuse remembered
	// merge resolutions while reconstructing its comparison tree.
	hooks := filepath.Join(temporary, "hooks")
	if err = os.Mkdir(hooks, 0o700); err != nil {
		return result, err
	}
	for _, setting := range [][2]string{{"core.hooksPath", hooks}, {"rerere.enabled", "false"}, {"core.autocrlf", "false"}, {"core.fsmonitor", "false"}} {
		if _, err = work.run(ctx, "config", "--local", setting[0], setting[1]); err != nil {
			return result, err
		}
	}
	if _, err = work.run(ctx, "checkout", "--quiet", "--detach", result.Tip); err != nil {
		return result, err
	}

	for i, commit := range history {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		progress(fmt.Sprintf("Isolating feature changes %d/%d · %s", i+1, len(history), commit.ShortID()))
		if imported[commit.ID] {
			if previous := evidence.previous[commit.ID]; previous != "" {
				result.Provenance = append(result.Provenance, CommitProvenance{ID: commit.ID, Role: integrationUpdate, Previous: previous})
			}
			result.Imported++
			continue
		}
		role := evidence.roles[commit.ID]
		if role == "" {
			role = featureCommit
			if len(commit.Parents) > 1 {
				role = integrationMerge
			}
		}
		result.Provenance = append(result.Provenance, CommitProvenance{ID: commit.ID, Role: role})
		if len(commit.Parents) > 1 {
			result.Merges++
			if len(commit.Parents) > 2 {
				return result, fmt.Errorf("merge %s has more than two parents; its provenance needs manual review", commit.ShortID())
			}
			patch, showErr := r.run(ctx, "show", "--remerge-diff", "--format=", "--no-ext-diff", "--no-color", commit.ID, "--")
			if showErr != nil {
				return result, showErr
			}
			if strings.Contains(patch, "diff --git ") {
				result.Uncertain = append(result.Uncertain, Uncertain{Commit: commit.ID, Subject: commit.Subject, Reason: "Merge resolution · historical changes", Patch: patch})
			}
			if role != featureMerge {
				continue
			}
		}

		args := []string{"revert", "--no-commit"}
		if len(commit.Parents) > 1 {
			args = append(args, "--mainline", "1")
		} else {
			result.Commits++
		}
		args = append(args, commit.ID)
		if _, revertErr := work.run(ctx, args...); revertErr != nil {
			unmerged, listErr := work.run(ctx, "ls-files", "--unmerged", "-z")
			if listErr != nil {
				return result, listErr
			}
			if unmerged == "" {
				return result, revertErr
			}
			paths := conflictedPaths(unmerged)
			args := append([]string{"diff", "--no-ext-diff", "--no-color", commit.Parents[0], commit.ID, "--"}, paths...)
			patch, patchErr := r.run(ctx, args...)
			if patchErr != nil {
				return result, patchErr
			}
			if patch == "" {
				// A subsequent rename may move the conflicted path away from
				// its name in this commit. Keep the feature commit available.
				patch, patchErr = r.run(ctx, "diff", "--no-ext-diff", "--no-color", commit.Parents[0], commit.ID, "--")
				if patchErr != nil {
					return result, patchErr
				}
			}
			result.Uncertain = append(result.Uncertain, Uncertain{Commit: commit.ID, Subject: commit.Subject, Reason: "Overlapping changes · historical feature patch", Patch: patch})

			// Retain the current background for ambiguous paths. The original
			// feature patch above remains available rather than being hidden.
			for _, path := range paths {
				if _, stageErr := work.run(ctx, "cat-file", "-e", ":2:"+path); stageErr == nil {
					if _, err = work.run(ctx, "checkout", "--ours", "--", path); err == nil {
						_, err = work.run(ctx, "add", "--", path)
					}
				} else {
					_, err = work.run(ctx, "rm", "--force", "--ignore-unmatch", "--", path)
				}
				if err != nil {
					return result, err
				}
			}
			if _, err = work.run(ctx, "revert", "--quit"); err != nil {
				return result, err
			}
		}
	}

	progress("Building complete feature diff…")
	baseline, err := work.run(ctx, "write-tree")
	if err != nil {
		return result, err
	}
	result.Patch, err = work.run(ctx, "diff", "--no-ext-diff", "--no-color", "--find-renames", "--unified=12", strings.TrimSpace(baseline), result.Tip, "--")
	if err == nil && options.KeepSnapshot {
		keep = true
		result.SnapshotPath = work.path
		result.Baseline = strings.TrimSpace(baseline)
	}
	return result, err
}

func conflictedPaths(output string) []string {
	seen := map[string]bool{}
	var paths []string
	for entry := range strings.SplitSeq(output, "\x00") {
		_, path, ok := strings.Cut(entry, "\t")
		if ok && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

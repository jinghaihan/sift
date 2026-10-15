package review

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type gitRepository struct {
	path string
}

func (r gitRepository) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.path, "-c", "core.quotepath=false", "-c", "color.ui=false"}, args...)...)
	var output, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return output.String(), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return output.String(), nil
}

func openRepository(ctx context.Context, path string) (gitRepository, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return gitRepository{}, err
	}
	r := gitRepository{path: absolute}
	root, err := r.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return r, err
	}
	r.path = strings.TrimSpace(root)
	return r, nil
}

func (r gitRepository) resolve(ctx context.Context, ref string) (string, error) {
	value, err := r.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(value), err
}

type Repository struct {
	Path     string
	Branch   string
	Branches []string
}

func Inspect(ctx context.Context, path string) (Repository, error) {
	r, err := openRepository(ctx, path)
	if err != nil {
		return Repository{}, err
	}
	refs, err := r.run(ctx, "for-each-ref", "--format=%(refname:short)%00%(symref)", "refs/heads/", "refs/remotes/")
	if err != nil {
		return Repository{}, err
	}
	branch, _ := r.run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(refs), "\n") {
		name, symbolic, ok := strings.Cut(line, "\x00")
		if ok && symbolic == "" {
			branches = append(branches, name)
		}
	}
	return Repository{Path: r.path, Branch: strings.TrimSpace(branch), Branches: branches}, nil
}

type Commit struct {
	ID      string
	Parents []string
	Subject string
	Date    string
}

func (c Commit) ShortID() string { return c.ID[:min(9, len(c.ID))] }

func History(ctx context.Context, path, branch string) ([]Commit, error) {
	r, err := openRepository(ctx, path)
	if err != nil {
		return nil, err
	}
	tip, err := r.resolve(ctx, branch)
	if err != nil {
		return nil, err
	}
	output, err := r.run(ctx, "log", "--topo-order", "--format=%H%x00%P%x00%s%x00%cs", tip, "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.SplitN(line, "\x00", 4)
		if len(fields) == 4 {
			commits = append(commits, Commit{ID: fields[0], Parents: strings.Fields(fields[1]), Subject: fields[2], Date: fields[3]})
		}
	}
	return commits, nil
}

// featureHistory follows the feature mainline through integration merges that
// were fast-forwarded back into the feature. Their feature-side parent retains
// the earlier work; following only first parents would lose it.
func featureHistory(history []Commit, start string, imported map[string]bool) ([]Commit, error) {
	byID := make(map[string]Commit, len(history))
	for _, commit := range history {
		byID[commit.ID] = commit
	}
	contains := map[string]bool{}
	for i := len(history) - 1; i >= 0; i-- {
		commit := history[i]
		contains[commit.ID] = commit.ID == start
		for _, parent := range commit.Parents {
			contains[commit.ID] = contains[commit.ID] || contains[parent]
		}
	}
	if len(history) == 0 || !contains[history[0].ID] {
		return nil, fmt.Errorf("the first feature commit is not an ancestor of this branch")
	}
	var result []Commit
	for commit := history[0]; ; {
		result = append(result, commit)
		if commit.ID == start {
			return result, nil
		}
		next := ""
		if imported[commit.ID] {
			for _, parent := range commit.Parents {
				if !imported[parent] && contains[parent] {
					next = parent
					break
				}
			}
		}
		if next == "" && len(commit.Parents) > 0 && contains[commit.Parents[0]] {
			next = commit.Parents[0]
		}
		if next == "" {
			return nil, fmt.Errorf("cannot trace the feature mainline through %s; select its actual first feature commit", commit.ShortID())
		}
		commit = byID[next]
	}
}

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PrepareWorktree clones the target repo into a fresh
// directory and checks out the appropriate branch for the
// run's mode.
//
// Fresh mode: clones the default branch and creates
// cfg.Branch as a new local branch. The CLI is expected to
// commit and push on that branch.
//
// Iterate mode: clones and checks out cfg.Branch directly.
// The CLI applies review-response commits on top.
//
// Returns the absolute path of the worktree. Callers set it
// as the CWD for the CLI shell-out.
func PrepareWorktree(ctx context.Context, cfg Config, parent string) (string, error) {
	if parent == "" {
		parent = os.TempDir()
	}
	worktree := filepath.Join(parent, "hubert-work-"+cfg.RunID)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		return "", fmt.Errorf("mkdir worktree: %w", err)
	}

	repoURL := fmt.Sprintf("https://github.com/%s.git", cfg.Repo)
	if err := runGit(ctx, "", "clone", "--depth", "50", repoURL, worktree); err != nil {
		return "", fmt.Errorf("clone %s: %w", cfg.Repo, err)
	}

	switch cfg.Mode {
	case "iterate":
		if cfg.Branch == "" {
			return "", fmt.Errorf("iterate mode requires a branch name")
		}
		if err := runGit(ctx, worktree, "fetch", "origin", cfg.Branch); err != nil {
			return "", fmt.Errorf("fetch %s: %w", cfg.Branch, err)
		}
		if err := runGit(ctx, worktree, "checkout", cfg.Branch); err != nil {
			return "", fmt.Errorf("checkout %s: %w", cfg.Branch, err)
		}
	case "fresh", "":
		if cfg.Branch != "" {
			if err := runGit(ctx, worktree, "checkout", "-b", cfg.Branch); err != nil {
				return "", fmt.Errorf("checkout -b %s: %w", cfg.Branch, err)
			}
		}
	default:
		return "", fmt.Errorf("unknown mode %q", cfg.Mode)
	}

	return worktree, nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

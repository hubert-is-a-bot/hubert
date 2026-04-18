// Package runner is the in-Job workflow driver. It implements
// the lock / heartbeat / CLI-invocation / lock-release lifecycle
// for one execution or reviewer run.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
	"github.com/hubert-is-a-bot/hubert/prompts"
)

// Config is the flag surface handed to Run by cmd/hubert-runner.
type Config struct {
	Role      string
	Repo      string
	Issue     int
	PR        int
	RunID     string
	Mode      string
	Iteration int
	Agent     string
	Model     string
	Branch    string
	BudgetUSD float64

	// HeartbeatInterval controls how often the lock comment
	// is refreshed. Zero defaults to 2 minutes. Tests set it
	// to something small.
	HeartbeatInterval time.Duration

	// CLI lets tests inject a fake invoker. Nil uses
	// DefaultCLIInvoker.
	CLI CLIInvoker

	// GH lets tests inject a fake GitHub client. Nil builds a
	// fresh Client via NewClient.
	GH *githubapi.Client
}

// ErrIterationCap is returned when HUBERT_ITERATION >= 3.
var ErrIterationCap = errors.New("runner: iteration cap reached")

// ErrLockRace is returned when the lock-acquisition call
// failed (someone else already holds the lock).
var ErrLockRace = errors.New("runner: lost lock race")

// ErrKillSwitchEngaged is returned when the repo carries a
// hubert-stop label at startup. The runner must exit 0 in this
// case — the re-check catches Jobs queued before the flip.
var ErrKillSwitchEngaged = errors.New("runner: kill switch engaged")

// Run is the top-level entrypoint. It acquires the issue lock,
// invokes the configured CLI with the matching embedded prompt,
// emits heartbeats on a timer, and releases the lock on exit.
//
// [Now] scope: single-backend claude shell-out with lock +
// heartbeat. Clone/commit/push/PR-open is [Now] task 3; when
// that lands, call it from runFresh below.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Iteration >= 3 {
		return ErrIterationCap
	}
	if cfg.RunID == "" {
		return errors.New("runner: HUBERT_RUN_ID is required")
	}
	if cfg.Repo == "" {
		return errors.New("runner: HUBERT_REPO is required")
	}
	issue := cfg.Issue
	if issue == 0 {
		issue = cfg.PR
	}
	if issue == 0 {
		return errors.New("runner: HUBERT_ISSUE or HUBERT_PR is required")
	}

	gh := cfg.GH
	if gh == nil {
		gh = githubapi.NewClient(cfg.Repo)
	}
	if err := checkKillSwitch(ctx, gh); err != nil {
		return err
	}

	lock, err := AcquireLock(ctx, gh, issue, cfg.RunID, cfg.Mode, cfg.Iteration)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLockRace, err)
	}

	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	var hbWg sync.WaitGroup
	hbWg.Add(1)
	go func() {
		defer hbWg.Done()
		runHeartbeat(hbCtx, lock, cfg.HeartbeatInterval)
	}()

	cwd, cwdErr := prepareCWD(ctx, cfg)
	var output string
	var runErr error
	if cwdErr != nil {
		runErr = cwdErr
	} else {
		output, runErr = invokeCLI(ctx, cfg, cwd)
	}
	stopHeartbeat()
	hbWg.Wait()

	hints := ParseRecoveryHints(output)
	tail := FormatRecoveryComment(hints)

	if runErr != nil {
		if rerr := lock.Release(context.Background(), "stopped", tail); rerr != nil {
			log.Printf("runner: release(stopped) comment: %v", rerr)
		}
		if uerr := lock.Unassign(context.Background()); uerr != nil {
			log.Printf("runner: unassign after stop: %v", uerr)
		}
		return runErr
	}
	if rerr := lock.Release(context.Background(), "complete", tail); rerr != nil {
		log.Printf("runner: release(complete) comment: %v", rerr)
	}
	return nil
}

func checkKillSwitch(ctx context.Context, gh *githubapi.Client) error {
	issues, err := gh.ListLabelIssues(ctx, "hubert-stop")
	if err != nil {
		return fmt.Errorf("kill-switch check: %w", err)
	}
	if len(issues) > 0 {
		return ErrKillSwitchEngaged
	}
	return nil
}

func runHeartbeat(ctx context.Context, lock *Lock, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := lock.Heartbeat(ctx, "running"); err != nil {
				log.Printf("runner: heartbeat: %v", err)
			}
		}
	}
}

// prepareCWD returns the working directory for the CLI. In
// the test path (cfg.CLI set), we return "" so the fake
// invoker doesn't care. In the production path, we clone the
// target repo into a scratch directory under $HOME/work and
// hand that path back. The K8s Job's emptyDir at /opt/data
// provides the scratch volume; $HOME is set to /opt/data in
// the image.
var prepareCWD = func(ctx context.Context, cfg Config) (string, error) {
	if cfg.CLI != nil {
		return "", nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = os.TempDir()
	}
	parent := filepath.Join(home, "work")
	return PrepareWorktree(ctx, cfg, parent)
}

func invokeCLI(ctx context.Context, cfg Config, cwd string) (string, error) {
	invoker := cfg.CLI
	if invoker == nil {
		invoker = DefaultCLIInvoker
	}
	prompt, err := selectPrompt(cfg.Role)
	if err != nil {
		return "", err
	}
	return invoker(ctx, CLIInvocation{
		Cwd:    cwd,
		Agent:  cfg.Agent,
		Model:  cfg.Model,
		Prompt: prompt,
	})
}

func selectPrompt(role string) (string, error) {
	switch role {
	case "execution", "":
		return prompts.Execution, nil
	case "reviewer":
		return prompts.Reviewer, nil
	case "orchestrator":
		return prompts.Orchestrator, nil
	default:
		return "", fmt.Errorf("unknown role %q", role)
	}
}

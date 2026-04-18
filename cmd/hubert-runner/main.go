// Command hubert-runner is the in-cluster entrypoint for
// Kubernetes Jobs that run an LLM CLI against one GitHub
// issue or PR. It acquires the issue lock, invokes the chosen
// CLI backend with the appropriate embedded prompt, emits
// heartbeats, and releases the lock on exit.
//
// This is the [Now] skeleton: it wires flags and delegates to
// internal/runner, which currently returns a not-implemented
// error. Real execution lands with §6 Task 3 in PLAN.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hubert-is-a-bot/hubert/internal/runner"
)

func main() {
	var cfg runner.Config
	flag.StringVar(&cfg.Role, "role", "", "runner role: execution or reviewer")
	flag.StringVar(&cfg.Repo, "repo", "", "target repository as owner/name")
	flag.IntVar(&cfg.Issue, "issue", 0, "issue number (execution runs)")
	flag.IntVar(&cfg.PR, "pr", 0, "pull request number (reviewer runs)")
	flag.StringVar(&cfg.RunID, "run-id", "", "ULID identifying this run")
	flag.StringVar(&cfg.Mode, "mode", "fresh", "execution mode: fresh or iterate")
	flag.IntVar(&cfg.Iteration, "iteration", 0, "iteration counter (0 for fresh)")
	flag.StringVar(&cfg.Agent, "agent", "claude", "CLI backend: claude, opencode, or gemini")
	flag.StringVar(&cfg.Model, "model", "", "model identifier for the chosen backend")
	flag.StringVar(&cfg.Branch, "branch", "", "branch name for this run")
	flag.Float64Var(&cfg.BudgetUSD, "budget-usd", 0, "hard cost cap for this run")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runner.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "hubert-runner: %v\n", err)
		os.Exit(1)
	}
}

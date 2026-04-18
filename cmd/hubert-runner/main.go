// Command hubert-runner is the in-cluster entrypoint for
// Kubernetes Jobs that run an LLM CLI against one GitHub
// issue or PR. It acquires the issue lock, invokes the chosen
// CLI backend with the appropriate embedded prompt, emits
// heartbeats, and releases the lock on exit.
//
// Config is read from HUBERT_* environment variables populated
// by the Job template in internal/dispatch. Flags are accepted
// as an override for local testing but are not used in the
// production Job path.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/hubert-is-a-bot/hubert/internal/runner"
)

func main() {
	cfg := configFromEnv()
	flag.StringVar(&cfg.Role, "role", cfg.Role, "runner role: execution or reviewer")
	flag.StringVar(&cfg.Repo, "repo", cfg.Repo, "target repository as owner/name")
	flag.IntVar(&cfg.Issue, "issue", cfg.Issue, "issue number (execution runs)")
	flag.IntVar(&cfg.PR, "pr", cfg.PR, "pull request number (reviewer runs)")
	flag.StringVar(&cfg.RunID, "run-id", cfg.RunID, "ULID identifying this run")
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "execution mode: fresh or iterate")
	flag.IntVar(&cfg.Iteration, "iteration", cfg.Iteration, "iteration counter (0 for fresh)")
	flag.StringVar(&cfg.Agent, "agent", cfg.Agent, "CLI backend: claude, opencode, or gemini")
	flag.StringVar(&cfg.Model, "model", cfg.Model, "model identifier for the chosen backend")
	flag.StringVar(&cfg.Branch, "branch", cfg.Branch, "branch name for this run")
	flag.Float64Var(&cfg.BudgetUSD, "budget-usd", cfg.BudgetUSD, "hard cost cap for this run")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if token := os.Getenv("HUBERT_GH_TOKEN"); token != "" {
		if err := runner.ConfigureGitAuth(ctx, token); err != nil {
			fmt.Fprintf(os.Stderr, "hubert-runner: configure git auth: %v\n", err)
			os.Exit(1)
		}
	}

	if err := runner.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "hubert-runner: %v\n", err)
		os.Exit(1)
	}
}

func configFromEnv() runner.Config {
	atoi := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}
	atof := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	mode := os.Getenv("HUBERT_MODE")
	if mode == "" {
		mode = "fresh"
	}
	agent := os.Getenv("HUBERT_AGENT")
	if agent == "" {
		agent = "opencode"
	}
	role := os.Getenv("HUBERT_ROLE")
	if role == "" {
		role = "execution"
	}
	return runner.Config{
		Role:      role,
		Repo:      os.Getenv("HUBERT_REPO"),
		Issue:     atoi(os.Getenv("HUBERT_ISSUE")),
		PR:        atoi(os.Getenv("HUBERT_PR")),
		RunID:     os.Getenv("HUBERT_RUN_ID"),
		Mode:      mode,
		Iteration: atoi(os.Getenv("HUBERT_ITERATION")),
		Agent:     agent,
		Model:     os.Getenv("HUBERT_MODEL"),
		Branch:    os.Getenv("HUBERT_BRANCH"),
		BudgetUSD: atof(os.Getenv("HUBERT_BUDGET_USD")),
	}
}

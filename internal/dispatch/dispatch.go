// Package dispatch turns orchestrator actions into Kubernetes
// Job applies (for dispatch-execution / dispatch-reviewer) and
// GitHub-side mutations (for reap-stale-lock / escalate / noop).
// It's the GHA-side half of the two-plane model: the workflow
// produces an action list, this package turns each entry into
// the matching side effect.
package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

// BotAccount is the GitHub login Hubert uses for issue
// assignment, comment authorship, and trust-gate checks.
// Mirrored in internal/runner; if this ever changes, the two
// must stay in sync.
const BotAccount = "hubert-is-a-bot"

// Config controls one dispatch invocation.
type Config struct {
	Repo           string
	Namespace      string
	Image          string
	ServiceAccount string
	RunID          string
	Branch         string
	BudgetUSD      float64
	ActionsFile    string

	// GH is the GitHub client used for reap-stale-lock,
	// escalate, and noop actions. nil builds a Client bound
	// to Repo via githubapi.NewClient.
	GH *githubapi.Client
	// Now is injected by tests; nil uses time.Now.
	Now func() time.Time
}

// Action is one orchestrator-emitted directive. The Name field
// selects behavior; the remaining fields are unmarshalled into
// json.RawMessage so each action type can parse only what it
// needs. See prompts/orchestrator.md for the wire format.
type Action struct {
	Name   string          `json:"action"`
	Fields json.RawMessage `json:"-"`
}

// UnmarshalJSON captures the full action object into Fields so
// individual action handlers can re-parse their own shape.
func (a *Action) UnmarshalJSON(data []byte) error {
	type header struct {
		Name string `json:"action"`
	}
	var h header
	if err := json.Unmarshal(data, &h); err != nil {
		return err
	}
	a.Name = h.Name
	a.Fields = append(a.Fields[:0], data...)
	return nil
}

type dispatchExecFields struct {
	Action    string `json:"action"`
	Issue     int    `json:"issue"`
	Mode      string `json:"mode"`
	Iteration int    `json:"iteration"`
	Agent     string `json:"agent"`
	Model     string `json:"model"`
	Tier      string `json:"tier"`
}

type dispatchReviewerFields struct {
	Action string `json:"action"`
	PR     int    `json:"pr"`
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Tier   string `json:"tier"`
}

type reapFields struct {
	Action string `json:"action"`
	Issue  int    `json:"issue"`
	RunID  string `json:"run_id"`
}

type escalateFields struct {
	Action string `json:"action"`
	Issue  int    `json:"issue"`
	Reason string `json:"reason"`
}

type noopFields struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// Apply turns each action into the appropriate side effect:
// dispatch-execution and dispatch-reviewer become Job applies;
// reap-stale-lock, escalate, and noop become GitHub-side
// mutations (comment + unassign / comment + label / log).
func Apply(ctx context.Context, cfg Config, actions []Action) error {
	for _, a := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := applyOne(ctx, cfg, a); err != nil {
			return fmt.Errorf("action %s: %w", a.Name, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, cfg Config, a Action) error {
	switch a.Name {
	case "dispatch-execution":
		return applyExecution(cfg, a)
	case "dispatch-reviewer":
		return applyReviewer(cfg, a)
	case "reap-stale-lock":
		return applyReap(ctx, cfg, a)
	case "escalate":
		return applyEscalate(ctx, cfg, a)
	case "noop":
		return applyNoop(a)
	default:
		return fmt.Errorf("unknown action kind %q", a.Name)
	}
}

// ghClient returns cfg.GH if set, otherwise builds a fresh
// Client bound to cfg.Repo. Tests override cfg.GH with an
// Exec-stubbed client; production leaves it nil.
func ghClient(cfg Config) *githubapi.Client {
	if cfg.GH != nil {
		return cfg.GH
	}
	return githubapi.NewClient(cfg.Repo)
}

// now returns the current time, respecting cfg.Now injection.
func now(cfg Config) time.Time {
	if cfg.Now != nil {
		return cfg.Now()
	}
	return time.Now()
}

func applyReap(ctx context.Context, cfg Config, a Action) error {
	var f reapFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse reap-stale-lock fields: %w", err)
	}
	if f.Issue == 0 {
		return fmt.Errorf("reap-stale-lock: missing issue")
	}
	gh := ghClient(cfg)
	ts := now(cfg).UTC().Format(time.RFC3339)
	body := fmt.Sprintf("🤖 hubert-reap %s stale %s\n\nPrevious run's heartbeat exceeded the 30-minute staleness window; releasing lock for retry.", f.RunID, ts)
	log.Printf("dispatch: reaping stale lock on %s#%d (run_id=%s)", cfg.Repo, f.Issue, f.RunID)
	if _, err := gh.PostComment(ctx, f.Issue, body); err != nil {
		return fmt.Errorf("post reap comment: %w", err)
	}
	if err := gh.UnassignIssue(ctx, f.Issue, BotAccount); err != nil {
		return fmt.Errorf("unassign bot: %w", err)
	}
	return nil
}

func applyEscalate(ctx context.Context, cfg Config, a Action) error {
	var f escalateFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse escalate fields: %w", err)
	}
	if f.Issue == 0 {
		return fmt.Errorf("escalate: missing issue")
	}
	gh := ghClient(cfg)
	ts := now(cfg).UTC().Format(time.RFC3339)
	reason := f.Reason
	if reason == "" {
		reason = "(no reason supplied)"
	}
	body := fmt.Sprintf("🤖 hubert-escalate %s: %s\n\nLabelling `hubert-stuck` and leaving the issue for a human to inspect.", ts, reason)
	log.Printf("dispatch: escalating %s#%d: %s", cfg.Repo, f.Issue, reason)
	if _, err := gh.PostComment(ctx, f.Issue, body); err != nil {
		return fmt.Errorf("post escalate comment: %w", err)
	}
	if err := gh.AddLabel(ctx, f.Issue, "hubert-stuck"); err != nil {
		return fmt.Errorf("add hubert-stuck label: %w", err)
	}
	return nil
}

func applyNoop(a Action) error {
	var f noopFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse noop fields: %w", err)
	}
	reason := f.Reason
	if reason == "" {
		reason = "(no reason)"
	}
	log.Printf("dispatch: noop: %s", reason)
	return nil
}

func applyExecution(cfg Config, a Action) error {
	var f dispatchExecFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse dispatch-execution fields: %w", err)
	}
	t, err := resolveTier(f.Tier)
	if err != nil {
		return err
	}
	jobName, err := newJobName("hubert-exec")
	if err != nil {
		return err
	}
	d := jobData{
		JobName:               jobName,
		Namespace:             cfg.Namespace,
		Image:                 cfg.Image,
		ServiceAccount:        cfg.ServiceAccount,
		RunID:                 cfg.RunID,
		Repo:                  cfg.Repo,
		Issue:                 f.Issue,
		PR:                    0,
		Agent:                 f.Agent,
		Model:                 f.Model,
		Mode:                  f.Mode,
		Role:                  "execution",
		Branch:                cfg.Branch,
		Iteration:             f.Iteration,
		BudgetUSD:             cfg.BudgetUSD,
		CPURequest:            t.CPURequest,
		CPULimit:              t.CPULimit,
		MemoryRequest:         t.MemoryRequest,
		MemoryLimit:           t.MemoryLimit,
		ActiveDeadlineSeconds: t.ActiveDeadlineSeconds,
	}
	manifest, err := renderJob(d)
	if err != nil {
		return err
	}
	log.Printf("dispatch: applying job %s/%s (tier=%s)", cfg.Namespace, jobName, f.Tier)
	return kubectlApply(cfg.Namespace, manifest)
}

func applyReviewer(cfg Config, a Action) error {
	var f dispatchReviewerFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse dispatch-reviewer fields: %w", err)
	}
	t, err := resolveTier(f.Tier)
	if err != nil {
		return err
	}
	jobName, err := newJobName("hubert-review")
	if err != nil {
		return err
	}
	d := jobData{
		JobName:               jobName,
		Namespace:             cfg.Namespace,
		Image:                 cfg.Image,
		ServiceAccount:        cfg.ServiceAccount,
		RunID:                 cfg.RunID,
		Repo:                  cfg.Repo,
		Issue:                 0,
		PR:                    f.PR,
		Agent:                 f.Agent,
		Model:                 f.Model,
		Mode:                  "reviewer",
		Role:                  "reviewer",
		Branch:                cfg.Branch,
		Iteration:             0,
		BudgetUSD:             cfg.BudgetUSD,
		CPURequest:            t.CPURequest,
		CPULimit:              t.CPULimit,
		MemoryRequest:         t.MemoryRequest,
		MemoryLimit:           t.MemoryLimit,
		ActiveDeadlineSeconds: t.ActiveDeadlineSeconds,
	}
	manifest, err := renderJob(d)
	if err != nil {
		return err
	}
	log.Printf("dispatch: applying job %s/%s (tier=%s)", cfg.Namespace, jobName, f.Tier)
	return kubectlApply(cfg.Namespace, manifest)
}

func resolveTier(name string) (Tier, error) {
	t, ok := tiers[name]
	if !ok {
		return Tier{}, fmt.Errorf("unknown tier %q (valid: small, medium, large, xlarge)", name)
	}
	return t, nil
}

func newJobName(prefix string) (string, error) {
	b := make([]byte, 5)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generating job suffix: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

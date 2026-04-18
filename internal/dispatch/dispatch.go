// Package dispatch turns orchestrator actions into Kubernetes
// Job applies. It's the GHA-side half of the two-plane model:
// the workflow produces an action list, this package submits
// the corresponding Jobs.
package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
)

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

// Apply turns each action into the appropriate side effect:
// dispatch-execution and dispatch-reviewer become Job applies;
// reap-stale-lock, escalate, and noop are skipped pending
// GitHub-API integration in a follow-up task.
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

func applyOne(_ context.Context, cfg Config, a Action) error {
	switch a.Name {
	case "dispatch-execution":
		return applyExecution(cfg, a)
	case "dispatch-reviewer":
		return applyReviewer(cfg, a)
	case "reap-stale-lock", "escalate", "noop":
		// TODO: task 3 GitHub-side handlers
		log.Printf("dispatch: skipping %s (not yet implemented)", a.Name)
		return nil
	default:
		return fmt.Errorf("unknown action kind %q", a.Name)
	}
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

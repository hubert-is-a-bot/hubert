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
	"strconv"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

// BotAccount is the GitHub login Hubert uses for issue
// assignment, comment authorship, and trust-gate checks.
// Mirrored in internal/runner; if this ever changes, the two
// must stay in sync.
const BotAccount = "hubert-is-a-bot"

// Execution target for dispatch-execution / dispatch-reviewer
// actions. The [Now] default is "gha" so Hubert can run
// without a Kubernetes cluster; "k8s" remains wired up for
// deployments that want resource isolation and tier sizing.
const (
	TargetGHA = "gha"
	TargetK8s = "k8s"
)

// Config controls one dispatch invocation.
type Config struct {
	Repo        string
	Branch      string
	BudgetUSD   float64
	ActionsFile string

	// Target selects the execution backend: "gha" (default)
	// triggers hubert-exec.yml via `gh workflow run`; "k8s"
	// renders a Job and shells to kubectl apply.
	Target string

	// K8s-only fields (ignored when Target=="gha").
	Namespace      string
	Image          string
	ServiceAccount string

	// GH is the GitHub client used for reap-stale-lock,
	// escalate, noop, and (in gha target) workflow triggers.
	// nil builds a Client bound to Repo via githubapi.NewClient.
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
		return applyExecution(ctx, cfg, a)
	case "dispatch-reviewer":
		return applyReviewer(ctx, cfg, a)
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

// runParams is the target-neutral parameter bag for one
// execution or reviewer run. It bridges the JSON action and
// the per-target (k8s/gha) trigger.
type runParams struct {
	Role      string
	Mode      string
	Issue     int
	PR        int
	Iteration int
	Agent     string
	Model     string
	Tier      string
	RunID     string
}

// newRunID returns a time-ordered, random-suffixed run ID. Each
// dispatched execution/reviewer gets its own — the orchestrator
// decides what to dispatch; the dispatcher assigns identity.
// Tests override this var to get deterministic values.
var newRunID = func() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(buf)
}

func applyExecution(ctx context.Context, cfg Config, a Action) error {
	var f dispatchExecFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse dispatch-execution fields: %w", err)
	}
	p := runParams{
		Role:      "execution",
		Mode:      f.Mode,
		Issue:     f.Issue,
		Iteration: f.Iteration,
		Agent:     f.Agent,
		Model:     f.Model,
		Tier:      f.Tier,
		RunID:     newRunID(),
	}
	return applyRun(ctx, cfg, p)
}

func applyReviewer(ctx context.Context, cfg Config, a Action) error {
	var f dispatchReviewerFields
	if err := json.Unmarshal(a.Fields, &f); err != nil {
		return fmt.Errorf("parse dispatch-reviewer fields: %w", err)
	}
	p := runParams{
		Role:  "reviewer",
		Mode:  "reviewer",
		PR:    f.PR,
		Agent: f.Agent,
		Model: f.Model,
		Tier:  f.Tier,
		RunID: newRunID(),
	}
	return applyRun(ctx, cfg, p)
}

func applyRun(ctx context.Context, cfg Config, p runParams) error {
	target := cfg.Target
	if target == "" {
		target = TargetGHA
	}
	switch target {
	case TargetGHA:
		return dispatchGHA(ctx, cfg, p)
	case TargetK8s:
		return dispatchK8s(cfg, p)
	default:
		return fmt.Errorf("unknown dispatch target %q (valid: gha, k8s)", target)
	}
}

func dispatchK8s(cfg Config, p runParams) error {
	t, err := resolveTier(p.Tier)
	if err != nil {
		return err
	}
	prefix := "hubert-exec"
	if p.Role == "reviewer" {
		prefix = "hubert-review"
	}
	jobName, err := newJobName(prefix)
	if err != nil {
		return err
	}
	d := jobData{
		JobName:               jobName,
		Namespace:             cfg.Namespace,
		Image:                 cfg.Image,
		ServiceAccount:        cfg.ServiceAccount,
		RunID:                 p.RunID,
		Repo:                  cfg.Repo,
		Issue:                 p.Issue,
		PR:                    p.PR,
		Agent:                 p.Agent,
		Model:                 p.Model,
		Mode:                  p.Mode,
		Role:                  p.Role,
		Branch:                cfg.Branch,
		Iteration:             p.Iteration,
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
	log.Printf("dispatch: applying k8s job %s/%s (role=%s tier=%s)", cfg.Namespace, jobName, p.Role, p.Tier)
	return kubectlApply(cfg.Namespace, manifest)
}

// dispatchGHA triggers the hubert-exec.yml workflow on the
// target repo via `gh workflow run`. Every runParams field is
// passed as an input (string-encoded) so the workflow's runner
// step can reconstruct the HUBERT_* env vars.
func dispatchGHA(ctx context.Context, cfg Config, p runParams) error {
	gh := ghClient(cfg)
	inputs := map[string]string{
		"role":       p.Role,
		"run_id":     p.RunID,
		"mode":       p.Mode,
		"iteration":  strconv.Itoa(p.Iteration),
		"issue":      strconv.Itoa(p.Issue),
		"pr":         strconv.Itoa(p.PR),
		"agent":      p.Agent,
		"model":      p.Model,
		"branch":     cfg.Branch,
		"budget_usd": strconv.FormatFloat(cfg.BudgetUSD, 'f', -1, 64),
	}
	log.Printf("dispatch: triggering gha workflow hubert-exec.yml on %s (role=%s issue=%d pr=%d)", cfg.Repo, p.Role, p.Issue, p.PR)
	return gh.TriggerWorkflow(ctx, "hubert-exec.yml", "main", inputs)
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

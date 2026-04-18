package dispatch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

type fakeExec struct {
	calls    [][]string
	response []byte
	err      error
}

func (f *fakeExec) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.response, f.err
}

func fakeCfg(resp []byte) (Config, *fakeExec) {
	fe := &fakeExec{response: resp}
	c := &githubapi.Client{Repo: "owner/name", GH: "gh", Exec: fe.exec}
	return Config{
		Repo: "owner/name",
		GH:   c,
		Now:  func() time.Time { return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC) },
	}, fe
}

func containsArg(calls [][]string, substr string) bool {
	for _, call := range calls {
		for _, a := range call {
			if strings.Contains(a, substr) {
				return true
			}
		}
	}
	return false
}

func TestRenderJob_EnvVarsAndLabels(t *testing.T) {
	d := jobData{
		JobName:               "hubert-exec-aabbcc",
		Namespace:             "hubert",
		Image:                 "ghcr.io/hubert-is-a-bot/hubert-runner:main",
		ServiceAccount:        "hubert-runner",
		RunID:                 "run-42",
		Repo:                  "acme/restaurhaunt",
		Issue:                 7,
		PR:                    0,
		Agent:                 "claude",
		Model:                 "opus",
		Mode:                  "execution",
		Role:                  "execution",
		Branch:                "main",
		Iteration:             1,
		BudgetUSD:             5.0,
		CPURequest:            "1",
		CPULimit:              "2",
		MemoryRequest:         "4Gi",
		MemoryLimit:           "8Gi",
		ActiveDeadlineSeconds: 7200,
	}

	out, err := renderJob(d)
	if err != nil {
		t.Fatalf("renderJob: %v", err)
	}

	if !strings.Contains(out, "apiVersion: batch/v1") {
		t.Error("missing apiVersion: batch/v1")
	}
	if !strings.Contains(out, "kind: Job") {
		t.Error("missing kind: Job")
	}

	labelChecks := []string{
		"hubert/run-id: run-42",
		"hubert/repo: acme/restaurhaunt",
		`hubert/issue: "7"`,
		"hubert/agent: claude",
		"hubert/mode: execution",
		"hubert/role: execution",
	}
	for _, c := range labelChecks {
		if !strings.Contains(out, c) {
			t.Errorf("missing label/value %q in rendered manifest", c)
		}
	}

	envChecks := []string{
		"HUBERT_RUN_ID",
		"HUBERT_REPO",
		"HUBERT_ISSUE",
		"HUBERT_PR",
		"HUBERT_MODE",
		"HUBERT_ITERATION",
		"HUBERT_AGENT",
		"HUBERT_MODEL",
		"HUBERT_BRANCH",
		"HUBERT_BUDGET_USD",
	}
	for _, e := range envChecks {
		if !strings.Contains(out, e) {
			t.Errorf("missing env var %q in rendered manifest", e)
		}
	}

	if !strings.Contains(out, "activeDeadlineSeconds: 7200") {
		t.Error("missing activeDeadlineSeconds: 7200")
	}
	if !strings.Contains(out, "cpu: 1") {
		t.Error("missing cpu limit/request")
	}
	if !strings.Contains(out, "memory: 4Gi") {
		t.Error("missing memory request")
	}
}

func TestTiers(t *testing.T) {
	const maxDeadline = 6 * 3600 // 6-hour admission ceiling
	for _, name := range []string{"small", "medium", "large", "xlarge"} {
		tier, ok := tiers[name]
		if !ok {
			t.Errorf("tier %q not found", name)
			continue
		}
		if tier.ActiveDeadlineSeconds <= 0 {
			t.Errorf("tier %q: non-positive deadline", name)
		}
		if tier.ActiveDeadlineSeconds > maxDeadline {
			t.Errorf("tier %q: deadline %d exceeds 6h ceiling (%d)", name, tier.ActiveDeadlineSeconds, maxDeadline)
		}
	}
}

func TestResolveTier_Unknown(t *testing.T) {
	_, err := resolveTier("gigantic")
	if err == nil {
		t.Error("expected error for unknown tier, got nil")
	}
}

func TestApplyReap(t *testing.T) {
	cfg, fe := fakeCfg([]byte(`{"id":1}`))
	raw := []byte(`{"action":"reap-stale-lock","issue":42,"run_id":"01HXYRUN"}`)
	var a Action
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := applyOne(context.Background(), cfg, a); err != nil {
		t.Fatalf("applyOne reap: %v", err)
	}
	if len(fe.calls) != 2 {
		t.Fatalf("want 2 gh calls (comment + unassign), got %d", len(fe.calls))
	}
	if !containsArg(fe.calls[:1], "repos/owner/name/issues/42/comments") {
		t.Errorf("first call should be PostComment; got %v", fe.calls[0])
	}
	if !containsArg(fe.calls[:1], "🤖 hubert-reap 01HXYRUN stale") {
		t.Errorf("comment body missing expected marker; got %v", fe.calls[0])
	}
	if !containsArg(fe.calls[1:], "repos/owner/name/issues/42/assignees") {
		t.Errorf("second call should be UnassignIssue; got %v", fe.calls[1])
	}
	if !containsArg(fe.calls[1:], "assignees[]=hubert-is-a-bot") {
		t.Errorf("unassign target should be the bot account; got %v", fe.calls[1])
	}
}

func TestApplyEscalate(t *testing.T) {
	cfg, fe := fakeCfg([]byte(`{"id":2}`))
	raw := []byte(`{"action":"escalate","issue":17,"reason":"iteration cap reached"}`)
	var a Action
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := applyOne(context.Background(), cfg, a); err != nil {
		t.Fatalf("applyOne escalate: %v", err)
	}
	if len(fe.calls) != 2 {
		t.Fatalf("want 2 gh calls (comment + label), got %d", len(fe.calls))
	}
	if !containsArg(fe.calls[:1], "repos/owner/name/issues/17/comments") {
		t.Errorf("first call should be PostComment; got %v", fe.calls[0])
	}
	if !containsArg(fe.calls[:1], "iteration cap reached") {
		t.Errorf("reason should appear in comment body; got %v", fe.calls[0])
	}
	if !containsArg(fe.calls[1:], "repos/owner/name/issues/17/labels") {
		t.Errorf("second call should be AddLabel; got %v", fe.calls[1])
	}
	if !containsArg(fe.calls[1:], "labels[]=hubert-stuck") {
		t.Errorf("label should be hubert-stuck; got %v", fe.calls[1])
	}
}

func TestApplyNoop(t *testing.T) {
	cfg, fe := fakeCfg(nil)
	raw := []byte(`{"action":"noop","reason":"kill switch engaged"}`)
	var a Action
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := applyOne(context.Background(), cfg, a); err != nil {
		t.Fatalf("applyOne noop: %v", err)
	}
	if len(fe.calls) != 0 {
		t.Errorf("noop should not touch gh; got %d calls", len(fe.calls))
	}
}

func TestApplyReapMissingIssue(t *testing.T) {
	cfg, _ := fakeCfg(nil)
	raw := []byte(`{"action":"reap-stale-lock","run_id":"X"}`)
	var a Action
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := applyOne(context.Background(), cfg, a); err == nil {
		t.Error("expected error for missing issue, got nil")
	}
}

func TestActionUnmarshal(t *testing.T) {
	raw := []byte(`[
		{"action":"dispatch-execution","issue":1,"mode":"execution","iteration":0,"agent":"claude","model":"opus","tier":"medium"},
		{"action":"noop"},
		{"action":"reap-stale-lock"}
	]`)
	var actions []Action
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
	if actions[0].Name != "dispatch-execution" {
		t.Errorf("expected dispatch-execution, got %q", actions[0].Name)
	}
	if actions[1].Name != "noop" {
		t.Errorf("expected noop, got %q", actions[1].Name)
	}
	if actions[2].Name != "reap-stale-lock" {
		t.Errorf("expected reap-stale-lock, got %q", actions[2].Name)
	}
	// Fields should be populated for dispatch-execution
	var f dispatchExecFields
	if err := json.Unmarshal(actions[0].Fields, &f); err != nil {
		t.Fatalf("unmarshal dispatch-execution fields: %v", err)
	}
	if f.Issue != 1 {
		t.Errorf("expected issue=1, got %d", f.Issue)
	}
	if f.Agent != "claude" {
		t.Errorf("expected agent=claude, got %q", f.Agent)
	}
	if f.Tier != "medium" {
		t.Errorf("expected tier=medium, got %q", f.Tier)
	}
}

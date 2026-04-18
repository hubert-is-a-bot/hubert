package dispatch

import (
	"encoding/json"
	"strings"
	"testing"
)

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

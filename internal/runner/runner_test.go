package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

func TestRunIterationCap(t *testing.T) {
	cfg := Config{RunID: "01H", Repo: "owner/name", Issue: 1, Iteration: 3}
	if err := Run(context.Background(), cfg); !errors.Is(err, ErrIterationCap) {
		t.Fatalf("want ErrIterationCap, got %v", err)
	}
}

func TestRunRequiresRepo(t *testing.T) {
	cfg := Config{RunID: "01H", Issue: 1}
	if err := Run(context.Background(), cfg); err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestRunLifecycle(t *testing.T) {
	// Fake gh transcript: kill-switch check returns empty,
	// lock comment post returns an id, assign returns ok,
	// heartbeat edit returns ok, final release posts a new
	// comment.
	var ghCalls atomic.Int32
	fe := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		ghCalls.Add(1)
		switch {
		case len(args) >= 2 && args[0] == "issue" && args[1] == "list":
			return []byte(`[]`), nil
		case containsEndpoint(args, "/comments") && hasFlag(args, "POST"):
			return []byte(`{"id": 999}`), nil
		case containsEndpoint(args, "/comments") && hasFlag(args, "PATCH"):
			return []byte(``), nil
		case containsEndpoint(args, "/assignees"):
			return []byte(``), nil
		default:
			return []byte(``), nil
		}
	}
	gh := &githubapi.Client{Repo: "owner/name", GH: "gh", Exec: fe}

	called := false
	fakeCLI := func(_ context.Context, inv CLIInvocation) (string, error) {
		called = true
		if inv.Agent != "claude" {
			t.Errorf("want agent=claude, got %s", inv.Agent)
		}
		if !strings.Contains(inv.Prompt, "execution agent") {
			t.Errorf("expected execution prompt; got first 80 chars: %q", inv.Prompt[:min(80, len(inv.Prompt))])
		}
		return "run complete\n", nil
	}

	cfg := Config{
		Role:              "execution",
		Repo:              "owner/name",
		Issue:             42,
		RunID:             "01HABCDEF",
		Mode:              "fresh",
		Iteration:         0,
		Agent:             "claude",
		Model:             "sonnet",
		HeartbeatInterval: 10 * time.Millisecond,
		CLI:               fakeCLI,
		GH:                gh,
	}
	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatal("CLI invoker was never called")
	}
	if ghCalls.Load() < 3 {
		t.Fatalf("expected ≥3 gh calls (kill-switch + lock post + assign + release), got %d", ghCalls.Load())
	}
}

func TestRunPropagatesCLIErrorWithHints(t *testing.T) {
	var released bool
	var releasedBody string
	fe := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch {
		case len(args) >= 2 && args[0] == "issue" && args[1] == "list":
			return []byte(`[]`), nil
		case containsEndpoint(args, "/comments") && hasFlag(args, "POST"):
			// Two POST /comments calls: the lock-acquire
			// comment and the stopped release comment. The
			// second contains the recovery-hint tail.
			body := flagValue(args, "body=")
			if strings.Contains(body, "stopped") {
				released = true
				releasedBody = body
			}
			return []byte(`{"id": 1}`), nil
		default:
			return []byte(``), nil
		}
	}
	gh := &githubapi.Client{Repo: "owner/name", GH: "gh", Exec: fe}
	fakeCLI := func(_ context.Context, _ CLIInvocation) (string, error) {
		return "out of memory\nneed-tier: larger\n", errors.New("oom-killed")
	}
	cfg := Config{
		Role:  "execution",
		Repo:  "owner/name",
		Issue: 42,
		RunID: "01HABC",
		Agent: "claude",
		CLI:   fakeCLI,
		GH:    gh,
	}
	if err := Run(context.Background(), cfg); err == nil {
		t.Fatal("expected propagated CLI error")
	}
	if !released {
		t.Fatal("expected a stopped release comment on CLI failure")
	}
	if !strings.Contains(releasedBody, "need-tier: larger") {
		t.Fatalf("release body missing hint: %q", releasedBody)
	}
}

func TestRunKillSwitchShortCircuits(t *testing.T) {
	fe := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "issue" && args[1] == "list" {
			return []byte(`[{"number": 1}]`), nil
		}
		return nil, errors.New("unexpected call; kill switch should have short-circuited")
	}
	gh := &githubapi.Client{Repo: "owner/name", GH: "gh", Exec: fe}
	cfg := Config{
		Role:  "execution",
		Repo:  "owner/name",
		Issue: 42,
		RunID: "01HABC",
		Agent: "claude",
		GH:    gh,
	}
	err := Run(context.Background(), cfg)
	if !errors.Is(err, ErrKillSwitchEngaged) {
		t.Fatalf("want ErrKillSwitchEngaged, got %v", err)
	}
}

func containsEndpoint(args []string, needle string) bool {
	for _, a := range args {
		if strings.Contains(a, needle) {
			return true
		}
	}
	return false
}

func hasFlag(args []string, v string) bool {
	for i, a := range args {
		if a == "-X" && i+1 < len(args) && args[i+1] == v {
			return true
		}
	}
	return false
}

func flagValue(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix)
		}
	}
	return ""
}

// silence unused-import warnings if json ever drops out.
var _ = json.Marshal

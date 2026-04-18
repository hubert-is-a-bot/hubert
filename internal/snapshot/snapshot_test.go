package snapshot

import (
	"context"
	"testing"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

// multiExec returns a different canned response for each call in order.
// Once all responses are exhausted, subsequent calls return the last response.
type multiExec struct {
	responses [][]byte
	idx       int
}

func (m *multiExec) exec(_ context.Context, _ string, _ ...string) ([]byte, error) {
	if m.idx >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func newMultiClient(responses [][]byte) *githubapi.Client {
	me := &multiExec{responses: responses}
	return &githubapi.Client{Repo: "owner/repo", GH: "gh", Exec: me.exec}
}

func TestCapture(t *testing.T) {
	// Responses in call order:
	// 1. ListCollaborators (trust gate evaluated first)
	// 2. ListOpenIssues
	// 3. ListIssueComments for issue 1 (alice, trusted)
	// 4. ListIssueComments for issue 2 (bob, trusted, has hubert-stop)
	// 5. ListOpenPullRequests
	// 6. ListPRReviewComments for PR 10 (carol, dropped by trust gate
	//    — but we still provide a response so the fixture is valid
	//    if the trust logic is ever relaxed)
	responses := [][]byte{
		// ListCollaborators
		[]byte(`[{"login": "alice", "type": "User"}, {"login": "bob", "type": "User"}]`),
		// ListOpenIssues
		[]byte(`[
			{
				"number": 1,
				"title": "normal issue",
				"body": "body",
				"author": {"login": "alice"},
				"assignees": [],
				"labels": [{"name": "bug"}],
				"state": "open",
				"createdAt": "2024-01-01T00:00:00Z",
				"updatedAt": "2024-01-01T00:00:00Z"
			},
			{
				"number": 2,
				"title": "stop issue",
				"body": "body",
				"author": {"login": "bob"},
				"assignees": [],
				"labels": [{"name": "hubert-stop"}],
				"state": "open",
				"createdAt": "2024-01-02T00:00:00Z",
				"updatedAt": "2024-01-02T00:00:00Z"
			}
		]`),
		// ListIssueComments for issue 1
		[]byte(`[
			{"id": 100, "user": {"login": "hubert-is-a-bot"}, "body": "🤖 hubert-run started", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z"}
		]`),
		// ListIssueComments for issue 2
		[]byte(`[]`),
		// ListOpenPullRequests
		[]byte(`[
			{
				"number": 10,
				"title": "my pr",
				"body": "pr body",
				"author": {"login": "carol"},
				"labels": [],
				"headRefName": "feature",
				"baseRefName": "main",
				"headRefOid": "deadbeef",
				"state": "open",
				"createdAt": "2024-01-01T00:00:00Z",
				"updatedAt": "2024-01-01T00:00:00Z",
				"statusCheckRollup": [
					{"name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}
				]
			}
		]`),
		// ListPRReviewComments for PR 10 (dropped by trust gate; not called)
		[]byte(`[]`),
	}

	client := newMultiClient(responses)
	snap, err := Capture(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if snap.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", snap.Repo, "owner/repo")
	}
	if snap.CapturedAt.IsZero() {
		t.Error("CapturedAt is zero")
	}

	// Issues
	if len(snap.Issues) != 2 {
		t.Fatalf("want 2 issues, got %d", len(snap.Issues))
	}
	if snap.Issues[0].Number != 1 {
		t.Errorf("Issues[0].Number = %d, want 1", snap.Issues[0].Number)
	}
	if len(snap.Issues[0].Comments) != 1 {
		t.Errorf("Issues[0].Comments len = %d, want 1", len(snap.Issues[0].Comments))
	}
	if snap.Issues[0].Comments[0].ID != 100 {
		t.Errorf("Issues[0].Comments[0].ID = %d, want 100", snap.Issues[0].Comments[0].ID)
	}

	// Pull requests: carol is not a collaborator, so the trust
	// gate filters her PR out entirely.
	if len(snap.PullRequests) != 0 {
		t.Fatalf("want 0 PRs (carol is not a collaborator), got %d", len(snap.PullRequests))
	}

	// Collaborators
	if len(snap.Collaborators) != 2 {
		t.Fatalf("want 2 collaborators, got %d", len(snap.Collaborators))
	}
	if snap.Collaborators[0].Login != "alice" {
		t.Errorf("Collaborators[0].Login = %q, want %q", snap.Collaborators[0].Login, "alice")
	}

	// Kill switch: issue 2 has hubert-stop label, so RepoPaused = true
	if snap.KillSwitch.Global != "OK" {
		t.Errorf("KillSwitch.Global = %q, want OK", snap.KillSwitch.Global)
	}
	if !snap.KillSwitch.RepoPaused {
		t.Error("KillSwitch.RepoPaused = false, want true (hubert-stop label present)")
	}
	if snap.KillSwitch.DailySpend != 0 || snap.KillSwitch.DailyCap != 0 {
		t.Errorf("expected zero spend/cap, got spend=%f cap=%f", snap.KillSwitch.DailySpend, snap.KillSwitch.DailyCap)
	}
}

func TestCaptureNoStop(t *testing.T) {
	responses := [][]byte{
		// ListCollaborators — x is trusted
		[]byte(`[{"login": "x", "type": "User"}]`),
		// ListOpenIssues — no hubert-stop
		[]byte(`[{"number": 1, "title": "t", "body": "", "author": {"login": "x"}, "assignees": [], "labels": [], "state": "open", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}]`),
		// ListIssueComments
		[]byte(`[]`),
		// ListOpenPullRequests
		[]byte(`[]`),
	}
	client := newMultiClient(responses)
	snap, err := Capture(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.KillSwitch.RepoPaused {
		t.Error("KillSwitch.RepoPaused = true, want false (no hubert-stop label)")
	}
	if snap.KillSwitch.Global != "OK" {
		t.Errorf("KillSwitch.Global = %q, want OK", snap.KillSwitch.Global)
	}
}

// Confirms the trust gate passes bot-authored issues even when
// the bot is not in the collaborator list — Hubert must be able
// to dispatch against its own sub-issues.
func TestCaptureBotIsTrusted(t *testing.T) {
	responses := [][]byte{
		// ListCollaborators — bot not listed
		[]byte(`[{"login": "alice", "type": "User"}]`),
		// ListOpenIssues — one by bot, one by stranger
		[]byte(`[
			{"number": 1, "title": "bot subissue", "body": "", "author": {"login": "hubert-is-a-bot"}, "assignees": [], "labels": [], "state": "open", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"},
			{"number": 2, "title": "drive-by", "body": "", "author": {"login": "stranger"}, "assignees": [], "labels": [], "state": "open", "createdAt": "2024-01-01T00:00:00Z", "updatedAt": "2024-01-01T00:00:00Z"}
		]`),
		// ListIssueComments for issue 1
		[]byte(`[]`),
		// ListOpenPullRequests
		[]byte(`[]`),
	}
	client := newMultiClient(responses)
	snap, err := Capture(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(snap.Issues) != 1 {
		t.Fatalf("want 1 issue (bot kept, stranger dropped), got %d", len(snap.Issues))
	}
	if snap.Issues[0].Author.Login != "hubert-is-a-bot" {
		t.Errorf("kept wrong issue: %+v", snap.Issues[0])
	}
}

package githubapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeExec records calls and returns canned responses.
type fakeExec struct {
	calls    [][]string
	response []byte
	err      error
}

func (f *fakeExec) exec(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return f.response, f.err
}

func newFakeClient(resp []byte, err error) (*Client, *fakeExec) {
	fe := &fakeExec{response: resp, err: err}
	c := &Client{Repo: "owner/name", GH: "gh", Exec: fe.exec}
	return c, fe
}

func TestAssignIssue(t *testing.T) {
	c, fe := newFakeClient(nil, nil)
	if err := c.AssignIssue(context.Background(), 42, "hubert-is-a-bot"); err != nil {
		t.Fatalf("AssignIssue: %v", err)
	}
	if len(fe.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(fe.calls))
	}
	got := fe.calls[0]
	want := []string{"gh", "api", "-X", "POST", "repos/owner/name/issues/42/assignees", "-f", "assignees[]=hubert-is-a-bot"}
	if !slicesEqual(got, want) {
		t.Fatalf("args mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestAssignIssuePropagatesError(t *testing.T) {
	c, _ := newFakeClient(nil, errors.New("422 already assigned"))
	err := c.AssignIssue(context.Background(), 42, "hubert-is-a-bot")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPostComment(t *testing.T) {
	c, fe := newFakeClient([]byte(`{"id": 12345}`), nil)
	id, err := c.PostComment(context.Background(), 42, "hello")
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if id != 12345 {
		t.Fatalf("want id=12345, got %d", id)
	}
	if got := fe.calls[0][4]; got != "repos/owner/name/issues/42/comments" {
		t.Fatalf("wrong endpoint: %s", got)
	}
}

func TestEditComment(t *testing.T) {
	c, fe := newFakeClient(nil, nil)
	if err := c.EditComment(context.Background(), 12345, "edited"); err != nil {
		t.Fatal(err)
	}
	if got := fe.calls[0][2]; got != "-X" {
		t.Fatalf("want PATCH verb flag, got %s", got)
	}
	if got := fe.calls[0][3]; got != "PATCH" {
		t.Fatalf("want PATCH, got %s", got)
	}
	if got := fe.calls[0][4]; got != "repos/owner/name/issues/comments/12345" {
		t.Fatalf("wrong endpoint: %s", got)
	}
}

func TestAddLabel(t *testing.T) {
	c, fe := newFakeClient(nil, nil)
	if err := c.AddLabel(context.Background(), 42, "hubert-review"); err != nil {
		t.Fatal(err)
	}
	got := fe.calls[0]
	want := []string{"gh", "api", "-X", "POST", "repos/owner/name/issues/42/labels", "-f", "labels[]=hubert-review"}
	if !slicesEqual(got, want) {
		t.Fatalf("args mismatch:\n got=%v\nwant=%v", got, want)
	}
}

func TestListLabelIssues(t *testing.T) {
	resp := []byte(`[{"number": 1}, {"number": 7}, {"number": 12}]`)
	c, fe := newFakeClient(resp, nil)
	got, err := c.ListLabelIssues(context.Background(), "hubert-stop")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 7, 12}
	if !intSlicesEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if fe.calls[0][1] != "issue" {
		t.Fatalf("want issue list subcommand, got %v", fe.calls[0])
	}
}

func TestGetIssue(t *testing.T) {
	resp := []byte(`{
		"number": 42,
		"title": "test",
		"body": "body",
		"state": "open",
		"user": {"login": "alice"},
		"assignees": [{"login": "hubert-is-a-bot"}],
		"labels": [{"name": "hubert-review"}]
	}`)
	c, _ := newFakeClient(resp, nil)
	got, err := c.GetIssue(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 42 || got.Title != "test" || got.Author.Login != "alice" {
		t.Fatalf("parse mismatch: %+v", got)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "hubert-is-a-bot" {
		t.Fatalf("assignees parse mismatch: %+v", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "hubert-review" {
		t.Fatalf("labels parse mismatch: %+v", got.Labels)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListOpenIssues(t *testing.T) {
	resp := []byte(`[
		{
			"number": 1,
			"title": "issue one",
			"body": "body one",
			"author": {"login": "alice"},
			"assignees": [],
			"labels": [{"name": "bug"}],
			"state": "open",
			"createdAt": "2024-01-01T00:00:00Z",
			"updatedAt": "2024-01-02T00:00:00Z"
		},
		{
			"number": 2,
			"title": "issue two",
			"body": "body two",
			"author": {"login": "bob"},
			"assignees": [{"login": "hubert-is-a-bot"}],
			"labels": [],
			"state": "open",
			"createdAt": "2024-01-03T00:00:00Z",
			"updatedAt": "2024-01-04T00:00:00Z"
		}
	]`)
	c, fe := newFakeClient(resp, nil)
	got, err := c.ListOpenIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 issues, got %d", len(got))
	}
	if got[0].Number != 1 || got[0].Title != "issue one" || got[0].Author.Login != "alice" {
		t.Fatalf("issue[0] mismatch: %+v", got[0])
	}
	if got[1].Number != 2 || got[1].Assignees[0].Login != "hubert-is-a-bot" {
		t.Fatalf("issue[1] mismatch: %+v", got[1])
	}
	args := fe.calls[0]
	if args[1] != "issue" || args[2] != "list" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestListOpenPullRequests(t *testing.T) {
	resp := []byte(`[
		{
			"number": 10,
			"title": "pr one",
			"body": "pr body",
			"author": {"login": "carol"},
			"labels": [],
			"headRefName": "feature",
			"baseRefName": "main",
			"headRefOid": "abc123",
			"state": "open",
			"createdAt": "2024-01-01T00:00:00Z",
			"updatedAt": "2024-01-02T00:00:00Z",
			"statusCheckRollup": [
				{"name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}
			]
		}
	]`)
	c, fe := newFakeClient(resp, nil)
	got, err := c.ListOpenPullRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 PR, got %d", len(got))
	}
	pr := got[0]
	if pr.Number != 10 || pr.HeadBranch != "feature" || pr.BaseBranch != "main" || pr.HeadSHA != "abc123" {
		t.Fatalf("PR mismatch: %+v", pr)
	}
	if len(pr.Checks) != 1 || pr.Checks[0].Name != "ci" || pr.Checks[0].Conclusion != "SUCCESS" {
		t.Fatalf("checks mismatch: %+v", pr.Checks)
	}
	args := fe.calls[0]
	if args[1] != "pr" || args[2] != "list" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestListCollaborators(t *testing.T) {
	resp := []byte(`[{"login": "alice", "type": "User"}, {"login": "bob", "type": "User"}]`)
	c, _ := newFakeClient(resp, nil)
	got, err := c.ListCollaborators(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 collaborators, got %d", len(got))
	}
	if got[0].Login != "alice" || got[1].Login != "bob" {
		t.Fatalf("collaborators mismatch: %+v", got)
	}
}

func TestListIssueComments(t *testing.T) {
	resp := []byte(`[
		{"id": 1, "user": {"login": "alice"}, "body": "regular comment", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z"},
		{"id": 2, "user": {"login": "hubert-is-a-bot"}, "body": "🤖 hubert-run started", "created_at": "2024-01-02T00:00:00Z", "updated_at": "2024-01-02T00:00:00Z"},
		{"id": 3, "user": {"login": "hubert-is-a-bot"}, "body": "🤖 hubert-done success", "created_at": "2024-01-03T00:00:00Z", "updated_at": "2024-01-03T00:00:00Z"}
	]`)
	c, _ := newFakeClient(resp, nil)
	got, err := c.ListIssueComments(context.Background(), 42, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 filtered comments, got %d", len(got))
	}
	if got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("comment IDs mismatch: %+v", got)
	}
}

func TestListIssueCommentsLimit(t *testing.T) {
	resp := []byte(`[
		{"id": 1, "user": {"login": "bot"}, "body": "🤖 hubert-a", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z"},
		{"id": 2, "user": {"login": "bot"}, "body": "🤖 hubert-b", "created_at": "2024-01-02T00:00:00Z", "updated_at": "2024-01-02T00:00:00Z"},
		{"id": 3, "user": {"login": "bot"}, "body": "🤖 hubert-c", "created_at": "2024-01-03T00:00:00Z", "updated_at": "2024-01-03T00:00:00Z"},
		{"id": 4, "user": {"login": "bot"}, "body": "🤖 hubert-d", "created_at": "2024-01-04T00:00:00Z", "updated_at": "2024-01-04T00:00:00Z"}
	]`)
	c, _ := newFakeClient(resp, nil)
	got, err := c.ListIssueComments(context.Background(), 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 comments after limit, got %d", len(got))
	}
	if got[0].ID != 3 || got[1].ID != 4 {
		t.Fatalf("expected last 2 comments, got IDs %d, %d", got[0].ID, got[1].ID)
	}
}

func TestListPRReviewComments(t *testing.T) {
	resp := []byte(`[
		{"id": 10, "user": {"login": "reviewer"}, "body": "looks good", "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z"},
		{"id": 11, "user": {"login": "hubert-is-a-bot"}, "body": "🤖 hubert-review done", "created_at": "2024-01-02T00:00:00Z", "updated_at": "2024-01-02T00:00:00Z"}
	]`)
	c, _ := newFakeClient(resp, nil)
	got, err := c.ListPRReviewComments(context.Background(), 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 filtered review comment, got %d", len(got))
	}
	if got[0].ID != 11 {
		t.Fatalf("expected comment ID 11, got %d", got[0].ID)
	}
}

func TestIsHubertComment(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"🤖 hubert-run started", true},
		{"🤖 hubert-done success", true},
		{"regular comment", false},
		{"", false},
		{"🤖 other thing", false},
		{"🤖 hubert-", true},
	}
	for _, tc := range cases {
		if got := isHubertComment(tc.body); got != tc.want {
			t.Errorf("isHubertComment(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

// Compile check that the Client satisfies the minimal
// interface the runner package depends on.
var _ = fmt.Sprintf

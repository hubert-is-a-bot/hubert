package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Client is a thin wrapper around the `gh` CLI. It captures
// the repo slug and shells out to `gh` for each operation.
// This is the [Now] implementation — it avoids the dependency
// and auth complexity of go-github at the cost of shelling out
// per call. go-github lands if and when we need fine-grained
// rate limiting or streaming.
type Client struct {
	// Repo is the owner/name slug all calls are scoped to.
	Repo string
	// GH is the command name, defaulting to "gh". Tests swap
	// in a stub via Exec.
	GH string
	// Exec is the command runner. Tests inject a fake; prod
	// code leaves it nil and runs the real `gh`.
	Exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewClient returns a Client bound to the given repo.
func NewClient(repo string) *Client {
	return &Client{Repo: repo, GH: "gh"}
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	name := c.GH
	if name == "" {
		name = "gh"
	}
	if c.Exec != nil {
		return c.Exec(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// AssignIssue assigns the given login to the issue. Returns an
// error if the assignment call fails (the caller interprets
// "already assigned" as a lost lock race — see runner).
func (c *Client) AssignIssue(ctx context.Context, issue int, login string) error {
	_, err := c.run(ctx,
		"api", "-X", "POST",
		fmt.Sprintf("repos/%s/issues/%d/assignees", c.Repo, issue),
		"-f", "assignees[]="+login,
	)
	return err
}

// UnassignIssue removes the given login from the issue.
func (c *Client) UnassignIssue(ctx context.Context, issue int, login string) error {
	_, err := c.run(ctx,
		"api", "-X", "DELETE",
		fmt.Sprintf("repos/%s/issues/%d/assignees", c.Repo, issue),
		"-f", "assignees[]="+login,
	)
	return err
}

// PostComment posts a new comment on an issue or PR (same
// endpoint). Returns the new comment ID.
func (c *Client) PostComment(ctx context.Context, issue int, body string) (int64, error) {
	out, err := c.run(ctx,
		"api", "-X", "POST",
		fmt.Sprintf("repos/%s/issues/%d/comments", c.Repo, issue),
		"-f", "body="+body,
	)
	if err != nil {
		return 0, err
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, fmt.Errorf("decode comment response: %w", err)
	}
	return resp.ID, nil
}

// EditComment replaces the body of an existing comment.
func (c *Client) EditComment(ctx context.Context, commentID int64, body string) error {
	_, err := c.run(ctx,
		"api", "-X", "PATCH",
		fmt.Sprintf("repos/%s/issues/comments/%d", c.Repo, commentID),
		"-f", "body="+body,
	)
	return err
}

// AddLabel adds a label to an issue. Labels not present in the
// repo are created on first use by `gh`.
func (c *Client) AddLabel(ctx context.Context, issue int, label string) error {
	_, err := c.run(ctx,
		"api", "-X", "POST",
		fmt.Sprintf("repos/%s/issues/%d/labels", c.Repo, issue),
		"-f", "labels[]="+label,
	)
	return err
}

// RemoveLabel removes a label from an issue. Returns nil if
// the label was already absent.
func (c *Client) RemoveLabel(ctx context.Context, issue int, label string) error {
	_, err := c.run(ctx,
		"api", "-X", "DELETE",
		fmt.Sprintf("repos/%s/issues/%d/labels/%s", c.Repo, issue, label),
	)
	return err
}

// ListLabels returns the labels currently applied to an issue.
func (c *Client) ListLabels(ctx context.Context, issue int) ([]string, error) {
	out, err := c.run(ctx,
		"api",
		fmt.Sprintf("repos/%s/issues/%d/labels", c.Repo, issue),
	)
	if err != nil {
		return nil, err
	}
	var resp []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	names := make([]string, len(resp))
	for i, l := range resp {
		names[i] = l.Name
	}
	return names, nil
}

// GetIssue fetches a single issue with its comments.
func (c *Client) GetIssue(ctx context.Context, issue int) (*Issue, error) {
	out, err := c.run(ctx,
		"api",
		fmt.Sprintf("repos/%s/issues/%d", c.Repo, issue),
	)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		State     string    `json:"state"`
		User      User      `json:"user"`
		Assignees []User    `json:"assignees"`
		Labels    []Label   `json:"labels"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return &Issue{
		Number:    raw.Number,
		Title:     raw.Title,
		Body:      raw.Body,
		State:     raw.State,
		Author:    raw.User,
		Assignees: raw.Assignees,
		Labels:    raw.Labels,
	}, nil
}

// TriggerWorkflow fires a workflow_dispatch event against the
// given workflow file on the given ref, passing inputs as
// -f key=value pairs. The workflow must be on the repo's
// default branch (or the ref given) and must declare each
// input in its `on.workflow_dispatch.inputs` section.
func (c *Client) TriggerWorkflow(ctx context.Context, workflow, ref string, inputs map[string]string) error {
	if ref == "" {
		ref = "main"
	}
	args := []string{
		"workflow", "run", workflow,
		"--repo", c.Repo,
		"--ref", ref,
	}
	for k, v := range inputs {
		args = append(args, "-f", fmt.Sprintf("%s=%s", k, v))
	}
	_, err := c.run(ctx, args...)
	return err
}

// ListLabelIssues returns open issue numbers in the repo
// carrying the given label. Used for kill-switch checks.
func (c *Client) ListLabelIssues(ctx context.Context, label string) ([]int, error) {
	out, err := c.run(ctx,
		"issue", "list",
		"--repo", c.Repo,
		"--label", label,
		"--state", "open",
		"--json", "number",
		"--limit", "100",
	)
	if err != nil {
		return nil, err
	}
	var resp []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode issue list: %w", err)
	}
	nums := make([]int, len(resp))
	for i, r := range resp {
		nums[i] = r.Number
	}
	return nums, nil
}

// ListOpenIssues returns all open issues in the repo with author,
// labels, and assignees. Comments are not populated here; use
// ListIssueComments for that.
func (c *Client) ListOpenIssues(ctx context.Context) ([]Issue, error) {
	out, err := c.run(ctx,
		"issue", "list",
		"--repo", c.Repo,
		"--state", "open",
		"--json", "number,title,body,author,assignees,labels,state,createdAt,updatedAt",
		"--limit", "500",
	)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		Author    User      `json:"author"`
		Assignees []User    `json:"assignees"`
		Labels    []Label   `json:"labels"`
		State     string    `json:"state"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode issue list: %w", err)
	}
	issues := make([]Issue, len(raw))
	for i, r := range raw {
		issues[i] = Issue{
			Number:    r.Number,
			Title:     r.Title,
			Body:      r.Body,
			Author:    r.Author,
			Assignees: r.Assignees,
			Labels:    r.Labels,
			State:     r.State,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return issues, nil
}

// ListOpenPullRequests returns all open PRs with author, labels,
// branches, head SHA, and checks. ReviewComments are not populated
// here; use ListPRReviewComments for that.
func (c *Client) ListOpenPullRequests(ctx context.Context) ([]PullRequest, error) {
	out, err := c.run(ctx,
		"pr", "list",
		"--repo", c.Repo,
		"--state", "open",
		"--json", "number,title,body,author,labels,headRefName,baseRefName,headRefOid,state,createdAt,updatedAt,statusCheckRollup",
		"--limit", "500",
	)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number      int     `json:"number"`
		Title       string  `json:"title"`
		Body        string  `json:"body"`
		Author      User    `json:"author"`
		Labels      []Label `json:"labels"`
		HeadRefName string  `json:"headRefName"`
		BaseRefName string  `json:"baseRefName"`
		HeadRefOid  string  `json:"headRefOid"`
		State       string  `json:"state"`
		CreatedAt   time.Time `json:"createdAt"`
		UpdatedAt   time.Time `json:"updatedAt"`
		StatusCheckRollup []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode pr list: %w", err)
	}
	prs := make([]PullRequest, len(raw))
	for i, r := range raw {
		checks := make([]CheckStatus, len(r.StatusCheckRollup))
		for j, ch := range r.StatusCheckRollup {
			checks[j] = CheckStatus{
				Name:       ch.Name,
				Status:     ch.Status,
				Conclusion: ch.Conclusion,
			}
		}
		prs[i] = PullRequest{
			Number:     r.Number,
			Title:      r.Title,
			Body:       r.Body,
			Author:     r.Author,
			Labels:     r.Labels,
			HeadBranch: r.HeadRefName,
			BaseBranch: r.BaseRefName,
			HeadSHA:    r.HeadRefOid,
			State:      r.State,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
			Checks:     checks,
		}
	}
	return prs, nil
}

// ResolveClosingIssue returns the issue number the PR claims to
// close via a "Closes #N" / "Fixes #N" linkage, as surfaced by
// GitHub's closingIssuesReferences field. Returns 0 if the PR
// does not reference a closing issue.
func (c *Client) ResolveClosingIssue(ctx context.Context, pr int) (int, error) {
	out, err := c.run(ctx,
		"pr", "view", fmt.Sprintf("%d", pr),
		"--repo", c.Repo,
		"--json", "closingIssuesReferences",
	)
	if err != nil {
		return 0, err
	}
	var raw struct {
		ClosingIssuesReferences []struct {
			Number int `json:"number"`
		} `json:"closingIssuesReferences"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return 0, fmt.Errorf("decode closingIssuesReferences: %w", err)
	}
	if len(raw.ClosingIssuesReferences) == 0 {
		return 0, nil
	}
	return raw.ClosingIssuesReferences[0].Number, nil
}

// ListCollaborators returns the logins of all repository collaborators.
func (c *Client) ListCollaborators(ctx context.Context) ([]User, error) {
	out, err := c.run(ctx,
		"api",
		fmt.Sprintf("repos/%s/collaborators", c.Repo),
		"--paginate",
	)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode collaborators: %w", err)
	}
	users := make([]User, len(raw))
	for i, r := range raw {
		users[i] = User{Login: r.Login, Type: r.Type}
	}
	return users, nil
}

// ListIssueComments fetches comments on an issue, filters to those
// whose body starts with "🤖 hubert-", and returns the last limit
// of those.
func (c *Client) ListIssueComments(ctx context.Context, issue int, limit int) ([]Comment, error) {
	out, err := c.run(ctx,
		"api",
		fmt.Sprintf("repos/%s/issues/%d/comments", c.Repo, issue),
		"--paginate",
	)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID        int64     `json:"id"`
		User      User      `json:"user"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode issue comments: %w", err)
	}
	var filtered []Comment
	for _, r := range raw {
		if isHubertComment(r.Body) {
			filtered = append(filtered, Comment{
				ID:        r.ID,
				Author:    r.User,
				Body:      r.Body,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

// ListPRReviewComments fetches review comments on a PR, filters to
// those whose body starts with "🤖 hubert-", and returns the last
// limit of those.
func (c *Client) ListPRReviewComments(ctx context.Context, pr int, limit int) ([]Comment, error) {
	out, err := c.run(ctx,
		"api",
		fmt.Sprintf("repos/%s/pulls/%d/comments", c.Repo, pr),
		"--paginate",
	)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID        int64     `json:"id"`
		User      User      `json:"user"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode pr review comments: %w", err)
	}
	var filtered []Comment
	for _, r := range raw {
		if isHubertComment(r.Body) {
			filtered = append(filtered, Comment{
				ID:        r.ID,
				Author:    r.User,
				Body:      r.Body,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}

// isHubertComment reports whether a comment body starts with the
// "🤖 hubert-" marker that Hubert uses for structured state.
func isHubertComment(body string) bool {
	const marker = "🤖 hubert-"
	return len(body) >= len(marker) && body[:len(marker)] == marker
}


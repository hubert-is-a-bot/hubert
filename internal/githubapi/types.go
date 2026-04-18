// Package githubapi defines the typed shape of GitHub state
// that Hubert operates on. These types match the JSON surface
// the orchestrator, runner, and snap binaries produce/consume.
//
// The mapping is deliberately minimal: only the fields Hubert's
// decision logic actually reads. If a future change needs more,
// add the field here rather than passing around raw maps.
package githubapi

import "time"

// User is the subset of a GitHub user identity Hubert cares
// about. Login is the handle used for trust and assignment
// decisions.
type User struct {
	Login string `json:"login"`
	Type  string `json:"type,omitempty"`
}

// Label is a GitHub label name. Hubert uses labels as its
// primary coordination channel (hubert-review,
// hubert-changes-requested, hubert-stuck, hubert-paused, etc.).
type Label struct {
	Name string `json:"name"`
}

// Comment is an issue or PR comment. Body is parsed for the
// structured 🤖 hubert-* markers that carry run lifecycle
// state across GHA ticks.
type Comment struct {
	ID        int64     `json:"id"`
	Author    User      `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Issue is an open GitHub issue in a watched repository.
type Issue struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Author     User      `json:"author"`
	Assignees  []User    `json:"assignees"`
	Labels     []Label   `json:"labels"`
	State      string    `json:"state"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Comments   []Comment `json:"comments"`
	LinkedPRs  []int     `json:"linked_prs,omitempty"`
}

// CheckStatus summarizes a single CI check on a PR's head SHA.
type CheckStatus struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

// PullRequest is an open PR in a watched repository. HeadSHA
// is the SHA reviewers must verify CI against; a stale green
// check on an old SHA is not approval-grade evidence.
type PullRequest struct {
	Number        int           `json:"number"`
	Title         string        `json:"title"`
	Body          string        `json:"body"`
	Author        User          `json:"author"`
	Labels        []Label       `json:"labels"`
	HeadBranch    string        `json:"head_branch"`
	BaseBranch    string        `json:"base_branch"`
	HeadSHA       string        `json:"head_sha"`
	State         string        `json:"state"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	Checks        []CheckStatus `json:"checks"`
	ReviewComments []Comment    `json:"review_comments"`
	ClosesIssue   int           `json:"closes_issue,omitempty"`
}

// KillSwitchState captures the three levels of stop Hubert
// honors. Global STOP wins over per-repo pause wins over daily
// cost cap. See PLAN.md §8.
type KillSwitchState struct {
	Global     string  `json:"global"`
	RepoPaused bool    `json:"repo_paused"`
	DailySpend float64 `json:"daily_spend"`
	DailyCap   float64 `json:"daily_cap"`
}

// Snapshot is the per-tick state blob produced by hubert-snap
// and consumed by the orchestrator prompt. One snapshot
// describes one repository at one moment in time.
type Snapshot struct {
	Repo          string          `json:"repo"`
	CapturedAt    time.Time       `json:"captured_at"`
	Collaborators []User          `json:"collaborators"`
	Issues        []Issue         `json:"issues"`
	PullRequests  []PullRequest   `json:"pull_requests"`
	KillSwitch    KillSwitchState `json:"kill_switch"`
}

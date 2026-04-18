// Package snapshot builds a per-tick GitHub state snapshot for
// one repository. It shells out to the `gh` CLI via a githubapi
// Client and assembles the Snapshot type the orchestrator prompt
// consumes.
package snapshot

import (
	"context"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

const (
	// hubertStopLabel is the label that signals a repo-level pause.
	hubertStopLabel = "hubert-stop"
	// recentCommentLimit is the number of filtered hubert comments
	// to include per issue or PR.
	recentCommentLimit = 5
)

// Capture builds and returns a Snapshot for the repository bound
// to client. It lists open issues (with recent hubert comments),
// open PRs (with recent review comments), collaborators, and
// evaluates the kill-switch state.
func Capture(ctx context.Context, client *githubapi.Client, repo string) (githubapi.Snapshot, error) {
	snap := githubapi.Snapshot{
		Repo:       repo,
		CapturedAt: time.Now().UTC(),
	}

	issues, err := client.ListOpenIssues(ctx)
	if err != nil {
		return snap, err
	}
	for i := range issues {
		comments, err := client.ListIssueComments(ctx, issues[i].Number, recentCommentLimit)
		if err != nil {
			return snap, err
		}
		issues[i].Comments = comments
	}
	snap.Issues = issues

	prs, err := client.ListOpenPullRequests(ctx)
	if err != nil {
		return snap, err
	}
	for i := range prs {
		comments, err := client.ListPRReviewComments(ctx, prs[i].Number, recentCommentLimit)
		if err != nil {
			return snap, err
		}
		prs[i].ReviewComments = comments
	}
	snap.PullRequests = prs

	collaborators, err := client.ListCollaborators(ctx)
	if err != nil {
		return snap, err
	}
	snap.Collaborators = collaborators

	snap.KillSwitch = githubapi.KillSwitchState{
		Global:     "OK",
		RepoPaused: repoPaused(snap.Issues),
		DailySpend: 0,
		DailyCap:   0,
	}

	return snap, nil
}

// repoPaused reports whether any open issue carries the
// hubert-stop label, which signals a repo-level pause.
func repoPaused(issues []githubapi.Issue) bool {
	for _, iss := range issues {
		for _, l := range iss.Labels {
			if l.Name == hubertStopLabel {
				return true
			}
		}
	}
	return false
}

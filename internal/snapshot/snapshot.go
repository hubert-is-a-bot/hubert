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
	// botLogin is the bot account whose own issues are always in
	// scope (Hubert can file sub-issues on itself). Must stay in
	// sync with internal/dispatch.BotAccount and internal/runner.LockBot.
	botLogin = "hubert-is-a-bot"
)

// Capture builds and returns a Snapshot for the repository bound
// to client. It lists open issues (with recent hubert comments),
// open PRs (with recent review comments), collaborators, and
// evaluates the kill-switch state. Issues and PRs authored by
// non-collaborators are dropped before the snapshot reaches the
// orchestrator — this is the trust gate, implemented in code so
// a prompt-injection payload in a stranger's issue never lands
// in the orchestrator's input.
func Capture(ctx context.Context, client *githubapi.Client, repo string) (githubapi.Snapshot, error) {
	snap := githubapi.Snapshot{
		Repo:       repo,
		CapturedAt: time.Now().UTC(),
	}

	collaborators, err := client.ListCollaborators(ctx)
	if err != nil {
		return snap, err
	}
	snap.Collaborators = collaborators
	trusted := trustSet(collaborators)

	issues, err := client.ListOpenIssues(ctx)
	if err != nil {
		return snap, err
	}
	trustedIssues := issues[:0]
	for _, iss := range issues {
		if !trusted[iss.Author.Login] {
			continue
		}
		comments, err := client.ListIssueComments(ctx, iss.Number, recentCommentLimit)
		if err != nil {
			return snap, err
		}
		iss.Comments = comments
		trustedIssues = append(trustedIssues, iss)
	}
	snap.Issues = trustedIssues

	prs, err := client.ListOpenPullRequests(ctx)
	if err != nil {
		return snap, err
	}
	trustedPRs := prs[:0]
	for _, pr := range prs {
		if !trusted[pr.Author.Login] {
			continue
		}
		comments, err := client.ListPRReviewComments(ctx, pr.Number, recentCommentLimit)
		if err != nil {
			return snap, err
		}
		pr.ReviewComments = comments
		trustedPRs = append(trustedPRs, pr)
	}
	snap.PullRequests = trustedPRs

	snap.KillSwitch = githubapi.KillSwitchState{
		Global:     "OK",
		RepoPaused: repoPaused(snap.Issues),
		DailySpend: 0,
		DailyCap:   0,
	}

	return snap, nil
}

// trustSet returns the set of logins that are in-scope for
// Hubert to act on: every collaborator plus the bot itself.
func trustSet(collabs []githubapi.User) map[string]bool {
	set := map[string]bool{botLogin: true}
	for _, u := range collabs {
		set[u.Login] = true
	}
	return set
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

package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hubert-is-a-bot/hubert/internal/githubapi"
)

// LockBot is the GitHub login Hubert assigns to locked issues.
// The assignment itself is the lock; the parallel comment is
// human-readable trace for reaping.
const LockBot = "hubert-is-a-bot"

// Lock is an in-process handle to a successfully acquired
// issue lock. The heartbeat comment ID is captured so the
// same comment can be edited in place rather than flooding
// the issue.
type Lock struct {
	client    *githubapi.Client
	issue     int
	runID     string
	commentID int64
}

// AcquireLock is the lock-acquisition protocol from
// prompts/execution.md. It:
//  1. Posts the lock-acquisition comment on the issue.
//  2. Assigns hubert-is-a-bot to the issue.
//
// If the assignment fails because someone else holds the lock,
// the caller treats it as a race loss. The caller is
// responsible for releasing the lock on exit.
func AcquireLock(ctx context.Context, c *githubapi.Client, issue int, runID, mode string, iteration int) (*Lock, error) {
	body := FormatLockComment(runID, mode, iteration, time.Now().UTC())
	id, err := c.PostComment(ctx, issue, body)
	if err != nil {
		return nil, fmt.Errorf("post lock comment: %w", err)
	}
	if err := c.AssignIssue(ctx, issue, LockBot); err != nil {
		return nil, fmt.Errorf("assign %s: %w", LockBot, err)
	}
	return &Lock{client: c, issue: issue, runID: runID, commentID: id}, nil
}

// Heartbeat edits the lock comment in place with a fresh
// timestamp and status line. Callers should invoke this on a
// 2-minute ticker and before any operation expected to take
// more than a couple minutes.
func (l *Lock) Heartbeat(ctx context.Context, status string) error {
	body := FormatHeartbeatComment(l.runID, time.Now().UTC(), status)
	return l.client.EditComment(ctx, l.commentID, body)
}

// Release posts the terminal lock-release comment. The exact
// body depends on the outcome (complete, stopped with hints,
// aborted). The assignment is NOT removed on success — the
// issue stays assigned to hubert-is-a-bot until the PR merges.
// Callers that need to forcibly unassign (e.g., stopped with
// no PR to follow up on) must call Unassign explicitly.
func (l *Lock) Release(ctx context.Context, kind, tail string) error {
	body := FormatReleaseComment(l.runID, kind, time.Now().UTC(), tail)
	_, err := l.client.PostComment(ctx, l.issue, body)
	return err
}

// Unassign is the explicit-unassign variant for the stopped /
// aborted paths where the issue needs to be freed for another
// run.
func (l *Lock) Unassign(ctx context.Context) error {
	return l.client.UnassignIssue(ctx, l.issue, LockBot)
}

// FormatLockComment renders the comment body for a fresh lock.
// Format is stable; the orchestrator's parser matches on the
// leading line.
func FormatLockComment(runID, mode string, iteration int, at time.Time) string {
	return fmt.Sprintf(
		"🤖 hubert-run %s started %s\nmode: %s\niteration: %d\n",
		runID,
		at.Format(time.RFC3339),
		mode,
		iteration,
	)
}

// FormatHeartbeatComment renders the comment body for a live
// heartbeat update. One status line must not contain newlines;
// callers that pass multi-line status collapse them here.
func FormatHeartbeatComment(runID string, at time.Time, status string) string {
	status = strings.ReplaceAll(status, "\n", " ")
	return fmt.Sprintf(
		"🤖 hubert-run %s heartbeat %s\nstatus: %s\n",
		runID,
		at.Format(time.RFC3339),
		status,
	)
}

// FormatReleaseComment renders a terminal comment. Kind is
// typically "complete", "stopped", or "aborted". Tail is
// appended after a blank line; for "complete" it's usually
// `pr: #N`, for "stopped" it's the recovery hint block, for
// "aborted" it's the reason.
func FormatReleaseComment(runID, kind string, at time.Time, tail string) string {
	head := fmt.Sprintf(
		"🤖 hubert-run %s %s %s\n",
		runID,
		kind,
		at.Format(time.RFC3339),
	)
	if tail == "" {
		return head
	}
	return head + tail
}

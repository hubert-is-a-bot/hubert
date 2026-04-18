package runner

import (
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)

func TestFormatLockComment(t *testing.T) {
	got := FormatLockComment("01HABCDEF", "fresh", 0, fixedTime)
	want := "🤖 hubert-run 01HABCDEF started 2026-04-18T12:00:00Z\nmode: fresh\niteration: 0\n"
	if got != want {
		t.Fatalf("want %q\ngot  %q", want, got)
	}
}

func TestFormatHeartbeatComment(t *testing.T) {
	got := FormatHeartbeatComment("01HABCDEF", fixedTime, "running tests")
	want := "🤖 hubert-run 01HABCDEF heartbeat 2026-04-18T12:00:00Z\nstatus: running tests\n"
	if got != want {
		t.Fatalf("want %q\ngot  %q", want, got)
	}
}

func TestFormatHeartbeatCollapsesNewlines(t *testing.T) {
	got := FormatHeartbeatComment("01H", fixedTime, "multi\nline")
	if strings.Count(got, "\n") > 2 {
		t.Fatalf("heartbeat body should have at most 2 newlines (after head, after status), got %d: %q", strings.Count(got, "\n"), got)
	}
}

func TestFormatReleaseComment(t *testing.T) {
	got := FormatReleaseComment("01H", "complete", fixedTime, "pr: #42\n")
	want := "🤖 hubert-run 01H complete 2026-04-18T12:00:00Z\npr: #42\n"
	if got != want {
		t.Fatalf("want %q\ngot  %q", want, got)
	}
}

func TestFormatReleaseCommentNoTail(t *testing.T) {
	got := FormatReleaseComment("01H", "aborted", fixedTime, "")
	want := "🤖 hubert-run 01H aborted 2026-04-18T12:00:00Z\n"
	if got != want {
		t.Fatalf("want %q\ngot  %q", want, got)
	}
}

package orchestrator

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractActionsJSONEmpty(t *testing.T) {
	out := "```hubert-actions\n[]\n```"
	body, err := ExtractActionsJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if body != "[]" {
		t.Fatalf("want [], got %q", body)
	}
}

func TestExtractActionsJSONWithActions(t *testing.T) {
	out := strings.Join([]string{
		"some preamble",
		"```hubert-actions",
		`[{"action": "noop", "reason": "nothing to do"}]`,
		"```",
		"trailing",
	}, "\n")
	body, err := ExtractActionsJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "\"action\"") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestExtractActionsJSONNoBlock(t *testing.T) {
	_, err := ExtractActionsJSON("no fenced block here")
	if !errors.Is(err, ErrNoActionsBlock) {
		t.Fatalf("want ErrNoActionsBlock, got %v", err)
	}
}

func TestExtractActionsJSONUnterminated(t *testing.T) {
	out := "```hubert-actions\n[]\nno close"
	_, err := ExtractActionsJSON(out)
	if err == nil {
		t.Fatal("expected unterminated error")
	}
}

func TestExtractActionsJSONInvalidJSON(t *testing.T) {
	out := "```hubert-actions\nnot json\n```"
	_, err := ExtractActionsJSON(out)
	if err == nil {
		t.Fatal("expected invalid-JSON error")
	}
}

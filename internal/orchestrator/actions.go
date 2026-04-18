// Package orchestrator parses the structured action list the
// orchestrator prompt emits in its `hubert-actions` fenced
// code block. The parser is intentionally strict: the
// orchestrator CLI's output feeds the workflow, and a silent
// parse failure would leave work un-dispatched.
package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoActionsBlock is returned when the orchestrator output
// contains no ```hubert-actions fenced block. That's a bug in
// the model's output, not an empty action list — an empty list
// is a well-formed `[]` inside the block.
var ErrNoActionsBlock = errors.New("orchestrator: no hubert-actions fenced block in output")

// ExtractActionsJSON pulls the first ```hubert-actions fenced
// code block out of the model's output and returns its raw
// JSON body. The caller is responsible for unmarshalling; this
// split lets the dispatch layer decode into its own Action
// type without introducing a cycle.
func ExtractActionsJSON(output string) (string, error) {
	const fence = "```hubert-actions"
	start := strings.Index(output, fence)
	if start < 0 {
		return "", ErrNoActionsBlock
	}
	rest := output[start+len(fence):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	}
	end := strings.Index(rest, "\n```")
	if end < 0 {
		return "", fmt.Errorf("orchestrator: unterminated hubert-actions block")
	}
	body := strings.TrimSpace(rest[:end])
	if !json.Valid([]byte(body)) {
		return "", fmt.Errorf("orchestrator: hubert-actions block is not valid JSON")
	}
	return body, nil
}

package prompts

import (
	"strings"
	"testing"
)

func TestPromptsEmbedded(t *testing.T) {
	for name, body := range map[string]string{
		"orchestrator": Orchestrator,
		"execution":    Execution,
		"reviewer":     Reviewer,
	} {
		if len(body) < 500 {
			t.Errorf("%s prompt looks suspiciously short (%d bytes); embed directive may be broken", name, len(body))
		}
		if !strings.Contains(body, "Hubert") {
			t.Errorf("%s prompt does not contain 'Hubert'; wrong file embedded?", name)
		}
	}
}

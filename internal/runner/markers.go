package runner

import (
	"regexp"
	"strings"
)

// RecoveryHint is the structured signal an escalating execution
// run leaves for the next orchestrator pass. See PLAN.md
// "Recovery pivots" table in prompts/orchestrator.md.
type RecoveryHint struct {
	Kind  string // "tier", "backend"
	Value string // "larger", "cheaper", "alternate"
}

// ParseRecoveryHints scans the CLI's stopped-state output for
// `need-backend: X` / `need-tier: Y` markers and returns each
// hint found. Unknown marker kinds are ignored. Empty result
// means the runner should escalate without a pivot hint (the
// orchestrator will treat it as plain hubert-stuck).
func ParseRecoveryHints(s string) []RecoveryHint {
	var hints []RecoveryHint
	re := regexp.MustCompile(`(?m)^need-(backend|tier):\s*(\S+)`)
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		hints = append(hints, RecoveryHint{
			Kind:  m[1],
			Value: strings.TrimSpace(m[2]),
		})
	}
	return hints
}

// FormatRecoveryComment renders a set of hints into the tail
// of a hubert-run stopped comment. Empty input returns an
// empty string so callers can unconditionally append.
func FormatRecoveryComment(hints []RecoveryHint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	for _, h := range hints {
		b.WriteString("need-")
		b.WriteString(h.Kind)
		b.WriteString(": ")
		b.WriteString(h.Value)
		b.WriteString("\n")
	}
	return b.String()
}

// Package budget tracks per-run and per-day cost. The [Now]
// skeleton exposes only the types; real accounting lands with
// §6 Task 6 in PLAN.md (hubert-log repo, emit-decision-record
// loop).
package budget

// Usage describes the cost a single LLM call incurred. The
// provider-specific price tables live in the runner; this
// package holds the currency-agnostic total.
type Usage struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	InputTokens int64   `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CachedTokens int64  `json:"cached_tokens"`
	USD         float64 `json:"usd"`
}

// Tally accumulates usage across a single run. Zero value is
// ready to use.
type Tally struct {
	Calls []Usage
	Total float64
}

// Add records one call against the tally.
func (t *Tally) Add(u Usage) {
	t.Calls = append(t.Calls, u)
	t.Total += u.USD
}

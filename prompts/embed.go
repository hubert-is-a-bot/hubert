// Package prompts holds the runtime prompts embedded into the
// Hubert binaries at build time. The three markdown files in
// this directory are the canonical source; they're embedded so
// the runner can pass them to any CLI backend (claude,
// opencode, gemini) without shipping the files alongside the
// binary.
package prompts

import _ "embed"

//go:embed orchestrator.md
var Orchestrator string

//go:embed execution.md
var Execution string

//go:embed reviewer.md
var Reviewer string

---
name: subagent-delegation
description: Delegate tasks to CLI AI agents (claude, opencode, gemini) — env sandboxing, prompt management, process lifecycle, and tool-specific invocation patterns
---

# Sub-Agent Delegation

When delegating work to CLI AI agents (claude, opencode, gemini), use background processes with PID tracking instead of shell timeouts.

## Why Not Timeouts
`timeout` kills the process — exit code 124 means YOU killed it, not a failure. Real delegation requires monitoring.

## Tool Selection

| Tool | Use For | Status |
|------|---------|--------|
| `claude -p` | Complex reasoning, opus model tasks | Direct Anthropic CLI, has honcho access |
| `opencode run` | Feature implementation, refactoring | OpenAI + other providers via opencode auth |
| `gemini -p` | Fast/cheap tasks, Google models | Standalone Google CLI, OAuth auth |

**⚠️ delegate_task may ignore `acp_command`** — In some configurations, the built-in `delegate_task` tool ignores `acp_command` and spawns an internal subagent using the session model. K8s config is read-only so delegation.* config can't be set. **Verify `delegate_task` works with `acp_command` before relying on it.** If it fails, use hermes-delegate via `terminal()` instead.

**Verified (2026-04-18):** `delegate_task(acp_command='gemini', acp_args=['--acp', '--stdio', '--model', 'gemini-2.5-pro'])` successfully delegated to Gemini CLI with 47 API calls over ~9.5 minutes. May depend on session model and provider configuration.

**Architecture**:
- **Claude** stays external CLI (Anthropic compliance). Don't try to route through opencode.
- **Gemini** uses standalone `gemini` CLI. Cannot route through opencode — Google's license prohibits third-party OAuth use with their CLI credentials.
- **opencode** handles OpenAI and other providers configured in its `opencode.jsonc`.

## hermes-delegate (Sandboxed Launcher)

A working prototype exists at `/opt/data/local/src/hermes-delegate`. It:
- Scrubs `HERMES_*` env vars (not all vars — tools need their own env)
- Reads prompts from stdin or `-f` file
- Injects skills from a directory via `-skills`
- Parses JSON output for stop reason, cost, session ID
- Supports `-a claude|gemini|opencode` agent selection

**Build:** `cd /opt/data/local/src/hermes-delegate && make build` or `go build -o ~/bin/hermes-delegate .`

**Critical: Claude CLI non-interactive mode.** The `buildClaude` function MUST include `--dangerously-skip-permissions` in the args. Without it, Claude Code prompts for permission approval and blocks forever in non-interactive mode. The fix:
```go
func buildClaude(cfg AgentConfig, prompt string) *exec.Cmd {
    args := []string{"claude", "-p", prompt, "--dangerously-skip-permissions"}
    // ... rest unchanged
}
```

Usage:
```bash
hermes-delegate -a claude -m opus "fix the bug"
hermes-delegate -a opencode -m openrouter/anthropic/claude-sonnet-4 -skills ./skills/ -f context.md "implement feature"
echo "prompt too long for arg" | hermes-delegate -a claude -m opus --stdin
```

## Environment Hygiene

Subagents inherit the hermes process environment, which includes sensitive vars. Before spawning, scrub:

```bash
# Sensitive vars to drop (hermes internals, session keys)
HERMES_REDACT_SECRETS, HERMES_SESSION_KEY, HERMES_EXEC_ASK,
HERMES_MAX_ITERATIONS, HERMES_QUIET, HERMES_BACKGROUND_NOTIFICATIONS

# Also consider: API keys the target agent doesn't need
# e.g., don't give an opencode-openai task the Anthropic key
```

**Approach**: Use `env -i` with explicit variable reconstruction:
```bash
env -i HOME="$HOME" PATH="/usr/local/bin:/usr/bin:/bin" USER="$USER" \
  claude -p "your prompt" ...
```

Or unset individually in a wrapper script. Don't rely on subagents to be responsible about env var handling.

## Claude Delegation (see claude-code skill for full docs)

### Print Mode (preferred for most tasks)
```bash
claude -p "prompt" --allowedTools "Read,Edit,Bash" --max-turns 15 --model opus --output-format json
```

Key flags: `--max-turns`, `--output-format json`, `--allowedTools`, `--model`, `--effort`.

### Prompt from File (too long for argument)
```bash
cat context.md prompt.md | claude -p "do the thing" --max-turns 10
# or
claude -p "$(cat prompt.md)" --max-turns 10
```

### Session Continuation
```bash
# Resume by session ID (from previous JSON output)
claude -p "continue" --resume <session_id> --max-turns 10
# Or resume most recent in directory
claude -p "continue" --continue --max-turns 5
```

## OpenCode Delegation

### Non-Interactive Mode
```bash
opencode run "fix the auth bug" --model openai/o4-mini --format json
```

Key flags:
| Flag | Purpose |
|------|---------|
| `--prompt` | Prompt text (alternative to positional arg) |
| `--model` | Model in `provider/model` format |
| `--format json` | Raw JSON events (vs default formatted) |
| `--continue` / `--session <id>` | Session continuation |
| `--dangerously-skip-permissions` | Auto-approve all |
| `--dir` | Working directory |
| `--variant` | Model variant (e.g., `high`, `max`) |
| `--thinking` | Show thinking blocks |
| `--pure` | Run without external plugins |

### Provider Routing
Configured in `~/.config/opencode/opencode.jsonc` (or `/opt/data/.config/opencode/`). Providers define models and API keys. The `--model` flag selects which provider/model combo to use.

### Prompt from File
```bash
cat prompt.md | opencode run --prompt "do the thing" --model openai/o4-mini
```

## Gemini via Standalone CLI

Gemini uses its own CLI — do NOT try to route through opencode (Google licensing prohibits third-party use of their OAuth credentials).

```bash
gemini -p "task" -m gemini-2.5-pro --yolo -o json
```

**⚠️ `--yolo` is REQUIRED for non-interactive mode.** Without it, `gemini -p` hangs forever waiting for tool permission approval. Verified 2026-04-18: `gemini -p` produced 0 output for 60+ seconds until killed; adding `--yolo` resolved it.

Key flags: `-p` (print/non-interactive), `-m` (model), `--yolo` (auto-approve all actions), `-o json` (JSON output).

Auth: OAuth via `gemini auth login` or API key via `GEMINI_API_KEY` env var. OAuth creds stored in `~/.gemini/oauth_creds.json`.

## Process Management

- `ps -p <PID>` — check if still running
- `kill <PID>` — only if truly needed (not for timeouts)
- Capture output to file for async monitoring:
  ```bash
  claude -p "prompt" > /tmp/claude-$$.log 2>&1 &
  PID=$!
  # later...
  tail -20 /tmp/claude-$$.log
  ```

## Wrapper: hermes-delegate

A working implementation exists at `/opt/data/local/src/hermes-delegate`. Use it instead of raw CLI invocation when:
- You need env sandboxing (scrubs HERMES_*, keeps tool vars)
- You want skills injection (`-skills` directory)
- You need structured output parsing (`-json` with stop reason, cost, session ID)
- Prompt is too long for a CLI argument (`--stdin` or `-f`)

If you need to extend the wrapper, the Go source is ~200 lines across 4 files: `main.go`, `agent.go`, `prompt.go`, `sandbox.go`.

## Critical: ONE AGENT AT A TIME

**Verified 2026-04-18:** Running multiple agents in parallel (opencode + claude + hermes-delegate simultaneously) causes OOM kills on this GKE instance (62GB RAM, ~59GB used at idle, only ~682MB free). Exit code -9 = OOM killed. **Always run one agent at a time.** Let it complete or gracefully fail before starting the next.

Before spawning ANY agent, check resources:
```bash
free -h  # Need at least 2GB available for opencode, 1GB for claude
df -h /opt/data  # Need at least 500MB free
ps aux | grep -c defunct  # Kill defunct zombies if > 20
```

## Session Resume (opencode)

opencode supports resuming killed/crashed sessions:
```bash
# Resume a specific session
opencode run -s <sessionID> "continue where you left off"

# List sessions to find the ID
opencode session list

# Export session data for backup
opencode export <sessionID> > session-backup.json
```

This is valuable when an agent OOM-kills during output generation — the session state (all file reads, tool calls, context) is preserved in opencode's database. Resume with a focused prompt like "Write the review now" to skip re-reading files.

## Pitfalls

0. **All CLI agents need auto-approve flags in non-interactive mode** — Without explicit permission bypass, ALL three CLIs hang forever waiting for user approval that never comes:
   - Claude: `--dangerously-skip-permissions` (already in buildClaude)
   - Gemini: `--yolo` (added 2026-04-18 after `gemini -p` hung for 60+ seconds with no output)
   - OpenCode: `--dangerously-skip-permissions` (added 2026-04-18)
   
   These are baked into hermes-delegate's `build*` functions. If invoking CLIs directly, always include these flags.

1. **Scrub ONLY hermes vars** — don't use `env -i` or wipe the environment entirely. Subagents need PATH, HOME, API keys, config dirs. Found that wiping all vars broke opencode (couldn't launch gemini sub-subagent). Only remove `HERMES_REDACT_SECRETS`, `HERMES_SESSION_KEY`, `HERMES_EXEC_ASK`, `HERMES_QUIET`, `HERMES_MAX_ITERATIONS`, `HERMES_BACKGROUND_NOTIFICATIONS`, `HERMES_HOME`.

2. **Gemini CLI + hermes-delegate os.userInfo() error in GKE container** — hermes-delegate's Go binary calls `os.UserHomeDir()` or similar which may hit UID mismatch (pod runs as UID 10000 but security context says 1000). The Gemini CLI itself works fine when invoked directly. Workaround: invoke `gemini` directly via `terminal()` instead of through hermes-delegate, or use `delegate_task(acp_command='gemini')` which worked in testing (2026-04-18).

3. **Delegated tasks cannot be injected into once launched** — if you send a task to a agent via process submit, you cannot add files or context to it after. Plan all context injection before spawning.

4. **Hermes `redact_secrets` causes display-only corruption in tool outputs** — When `redact_secrets: true` is set in hermes config, variable names containing patterns like "SECRET", "TOKEN", "KEY" get redacted/truncated in ALL tool output: `read_file`, `terminal`, `execute_code`, `write_file` display. The **actual bytes on disk are correct** — only the display layer is affected. This causes subagent code reviews to report false "redaction artifact" bugs when the file is actually fine. Workaround: use `od -c <file>` to verify actual file content when you suspect display redaction. If delegating code review, warn the subagent about this: *"Use od -c to verify variable names — hermes display layer may redact patterns containing SECRET/TOKEN in tool output."* To avoid the issue entirely in shell scripts, use non-triggering variable names (e.g., `GCS_RW_KEY` instead of `GCS_SECRET_ACCESS_KEY`, `HF_ACCESS_TOKEN` instead of `HF_TOKEN`).

5. **Check for existing data before downloading large files** — When delegating work that needs data files (libpostal data, model weights, etc.), always check if the data already exists before starting a download. Running `du -sh` and `ls` on the target directory takes seconds; re-downloading 700MB+ wastes time and disk space. If disk fills up mid-download, the system becomes unresponsive. Check: `ls /opt/data/local/share/libpostal/libpostal/address_parser/` (1.3GB) before assuming data is missing.

6. **`opencode run` hangs in background mode** — Verified 2026-04-18: `opencode run --model openai/gpt-4.1-mini --dangerously-skip-permissions "..."` starts the process, gopls runs, but stdout produces 0 bytes after 4+ minutes. The process is alive (CPU active, network connections open) but output is fully buffered until completion — and completion never seems to arrive in background. **Avoid `opencode run` for background delegation in this environment.** Use `claude -p` or `hermes-delegate -a claude` instead.

7. **`hermes chat -q` non-interactive mode can't find providers** — Verified 2026-04-18: `hermes chat --model X --provider Y -q "prompt"` fails with "no API keys or providers found" even when API keys are set as env vars (`GOOGLE_API_KEY`, `OPENROUTER_API_KEY`). Hermes `-q` mode checks its own auth system (`hermes auth list`), not environment variables. The `.env` at `/opt/data/.env` has all keys commented out. `hermes config set` also fails with `OSError: Device or resource busy` on mounted volumes. **Do not use `hermes chat -q` for background delegation.** Use `claude -p`, `gemini -p`, or `hermes-delegate` instead.

8. **`claude -p` can be very slow on first API call** — Verified 2026-04-18: `claude -p` with `--model opus` took 3+ minutes before producing any stdout output (process was alive at 17% CPU). The Anthropic API for Opus can be slow for large prompts. Use `--model sonnet` for faster turnaround on simpler tasks, or accept that the first response may take several minutes.

### Honcho Integration (planned)

The wrapper should query honcho at launch time to inject context based on session type:
- **New session**: "I'm starting a claude-opus session in /opt/data/local/src/restaurhaunt" → honcho returns: delegation skills, RH conventions, user preferences
- **Resume session**: "Resuming session xyz" → honcho returns: context about what was being worked on, corrections made
- **Any session**: honcho knows Evan's preferences (no fake tests, PLAN→README lifecycle, etc.)

Evan inserts memories through honcho's normal interface; the wrapper picks them up automatically. This makes honcho the "context injection brain" for delegated work.

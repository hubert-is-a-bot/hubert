# Hubert image contract

> Status: research-phase design. Defines the interface between
> Hubert (upstream) and a Hubert deployment's execution image
> (downstream). This is what a deployer must provide for Hubert's
> execution and reviewer Jobs to run.

## Why this document exists

Hubert is structured as **upstream code** (the runner binary, the
Job submission helper, the prompts, the GHA workflow templates)
and **downstream deployments** (one per operator: Evan's personal
setup, a hypothetical team deployment, a future open-source
user). Upstream ships an executable and a contract. Each
deployment satisfies the contract with its own container image.

The motivation, restated from the decision in ARCHITECTURE.md:
Hubert has no business shipping an image. Operators need to
choose their own base image, pin their own CLI tool versions,
control which LLM CLIs they authorize, and mount their own
credentials. Shipping an image upstream would either force those
choices on operators or bloat the image with everything every
operator might want.

The separation also means the runner binary and the per-project
`.hubert/README.md` can stay agnostic: Hubert doesn't care
whether your image has `opencode` or not; if the orchestrator
dispatches a task to opencode and the image doesn't have it, the
runner fails the Job cleanly and the next orchestrator pass sees
the failure via GitHub state.

## The contract

A Hubert execution image MUST provide:

### 1. Binaries on `$PATH`

- `hubert-runner` — the runner binary shipped by upstream. The
  Job's entrypoint invokes this with arguments describing the
  task (issue number, mode, agent, model, tier). See "Runner
  invocation" below.
- `git` — for cloning, committing, pushing.
- `gh` — GitHub CLI, used by the runner for lock operations
  (assignment, heartbeat comments, label writes, PR creation).
- At least one of the supported LLM CLIs, matching whichever
  the orchestrator will dispatch to:
  - `claude` — Anthropic Claude Code CLI, non-interactive via
    `claude --print`. Expected to be present by default; every
    reference deployment will have this.
  - `opencode` — OpenCode CLI, non-interactive via
    `opencode run <prompt> --dangerously-skip-permissions`.
    Optional. Enables dispatch to OpenAI models and free
    models (BigPickle, Nemotron, etc.) via OpenCode.
  - `gemini` — Google Gemini CLI, non-interactive via
    `gemini -p <prompt> --yolo`. Optional. Enables dispatch
    for tasks needing Google Search.

The orchestrator's dispatch actions name a backend
(`agent=claude|opencode|gemini`). The runner invokes that
backend's CLI. If the CLI is missing, the runner exits non-zero
with a clear error; the orchestrator's next pass sees the
failure and either retries with a different backend or
escalates.

### 2. Environment variables

Hubert's Job template injects:

- `HUBERT_RUN_ID` — unique identifier for this run (used in
  heartbeat comments, branch names, log correlation).
- `HUBERT_REPO` — `owner/name` of the target repo.
- `HUBERT_ISSUE` or `HUBERT_PR` — the issue or PR number being
  worked on.
- `HUBERT_MODE` — `execution` or `reviewer`.
- `HUBERT_AGENT` — `claude` / `opencode` / `gemini`.
- `HUBERT_MODEL` — model identifier to pass through to the CLI
  (e.g., `opus`, `opencode/big-pickle`, `gemini-2.5-pro`).
- `HUBERT_TIER` — tier name (informational; the actual resource
  limits are set at admission time by the policy).
- `HUBERT_BUDGET_USD` — per-run cost cap.
- `HUBERT_WORKTREE` — directory the runner should clone into
  (typically an emptyDir mount).

The image is also expected to receive credentials via a secret
mount configured by the deployment:

- `GITHUB_TOKEN` or equivalent — a PAT with push/comment
  permission on the target repo(s). Used by `gh` and `git push`.
- Provider-specific credentials for whichever CLIs the image
  ships (e.g., `ANTHROPIC_API_KEY` for `claude`, OpenCode's
  credential file under `$HOME/.config/opencode/auth.json`, a
  Google credential for `gemini`).

The specific secret layout is deployment-defined; Hubert just
asserts "the CLIs must be able to authenticate when invoked."

### 3. Writable scratch

The runner expects a writable directory at `$HUBERT_WORKTREE`
(defaults to `/workspace`). The deployment mounts this as an
emptyDir or equivalent ephemeral volume. It must be:

- Empty at Job start.
- Writable by the Job's user.
- At least 8Gi on standard deployments (the hermetic admission
  policy enforces this ceiling; smaller is fine for lightweight
  projects).

Anything written here dies with the pod. The runner does all
its work inside this directory; it MUST NOT write outside it.

### 4. Network egress

The Job needs egress to:

- `github.com` / `api.github.com` / `ghcr.io` — clone, push,
  API calls.
- `api.anthropic.com` — if shipping `claude`.
- `api.openai.com` / `opencode.ai` / `openrouter.ai` — if
  shipping `opencode`.
- `generativelanguage.googleapis.com` — if shipping `gemini`.

No inbound connections are required. Cluster-level egress
policy (e.g., NetworkPolicy) is a deployment concern; the
runner doesn't assume anything is blocked.

### 5. User / filesystem posture

- The Job SHOULD run as non-root. hermetic's admission policy
  rejects privileged containers; other deployments should
  match.
- The image SHOULD have `/tmp` available and writable; some
  CLIs use it for intermediate files.
- The image MAY have a read-only root filesystem, provided
  `$HUBERT_WORKTREE` and `/tmp` are writable mounts.

## Runner invocation

The Job's `command` / `args` invokes:

```
hubert-runner \
    --mode=${HUBERT_MODE} \
    --agent=${HUBERT_AGENT} \
    --model=${HUBERT_MODEL} \
    --repo=${HUBERT_REPO} \
    --issue=${HUBERT_ISSUE} \
    --run-id=${HUBERT_RUN_ID} \
    --budget-usd=${HUBERT_BUDGET_USD} \
    --worktree=${HUBERT_WORKTREE}
```

The runner reads its prompt (execution or reviewer) from
embedded strings compiled into the binary — no prompt-file
mount is required. This keeps the runner self-contained and
deployment-independent.

## What Hubert upstream does NOT prescribe

- The base image. `alpine`, `debian`, `ubuntu`, `distroless`,
  `wolfi` — deployer's choice.
- Which CLIs to include. A minimal deployment might ship only
  `claude`. A cost-optimized deployment might ship all three
  plus `openrouter` credentials.
- How to mount credentials. K8s Secret + `envFrom`, IRSA on
  EKS, Workload Identity on GKE, a sidecar fetching from
  Vault — all fine. The runner reads from env vars; the
  deployment decides how env vars get populated.
- Image registry, tag strategy, or build pipeline. GHA,
  GitLab CI, plain Dockerfile + manual push — deployer's
  choice.
- Runtime resource limits beyond the admission-policy
  ceilings. The admission policy caps; deployers pick defaults
  within the caps.

## Reference deployment: `hermetic`

The `../hermetic` repo is Evan's reference deployment. Its
image bundles all three CLIs, an Anthropic + OpenAI + Google
credential set, and runs on a dedicated GKE cluster with GCS
Fuse available (though Hubert doesn't use it). Operators
wanting a working starting point can copy hermetic's Dockerfile
and values.yaml and modify from there.

See [`HERMETIC-HANDOFF.md`](HERMETIC-HANDOFF.md) for what ports
cleanly from hermetic's infrastructure into a new deployment,
and what to skip.

## Failure modes and what the contract guarantees

If a deployment's image violates the contract, Hubert will
detect the violation as a Job failure and surface it via GitHub
state, not crash silently:

- Missing CLI → runner exits with `agent not found`, posts an
  escalation comment naming the missing binary.
- Missing credential → CLI invocation fails, runner captures
  the error and posts an escalation comment.
- Worktree not writable → runner fails at clone step, posts
  an escalation comment.
- Admission policy rejects the Job (tier too big, unsafe
  pod spec) → Job never starts, the next orchestrator pass
  sees `kubectl get job` failure and treats it as implicit
  escalation.

The contract is loose enough that operators have real choices
and tight enough that violations surface as legible errors
rather than mysterious hangs.

# Handoff: portable infrastructure from hermetic

> Status: research-phase design document, written 2026-04-18.
> The infrastructure described here is built, deployed, and
> running in `../hermetic` on the GKE cluster `ekdromos`
> (project `forklore-491503`). This file is the handoff note
> for whoever picks up Hubert implementation and needs to know
> what already exists versus what needs to be built.

## One-paragraph summary

The "Container/VM sandboxing for execution agents" item in
[`ARCHITECTURE.md`](ARCHITECTURE.md)'s deferred list is
already implemented in the hermetic repository. It takes the
shape of a Go binary that submits a Kubernetes Job at a fixed
resource tier, a `ValidatingAdmissionPolicy` that enforces
the Job template shape, namespaced RBAC for the submitter
ServiceAccount, and a Workload Identity setup script. Most
of it ports to Hubert with label renames; the CLI-agent-
specific layer (hermes-delegate's `agent.go` / `prompt.go`)
does not port because Hubert's execution agent is structured
differently.

## The hermetic repository

Path: `../hermetic`  (i.e., `/var/home/.../local/src/hermetic`
if you're on the same machine; otherwise `github.com/nemequ/
hermetic`, private).

Key files for this handoff:

- `docker/tools/hermes-delegate/` — the Go binary.
- `charts/hermes-agent/templates/rbac.yaml` — namespaced Role
  + RoleBinding.
- `charts/hermes-agent/templates/delegate-admission-policy.yaml`
  — the `ValidatingAdmissionPolicy` + Binding.
- `charts/hermes-agent/templates/serviceaccount.yaml` — the
  two K8s SAs (main + fsq sync), each annotated for Workload
  Identity.
- `scripts/setup-gcs-fsq.sh` — idempotent GCP setup: buckets,
  SAs, WI bindings, GH Actions secret distribution.
- `docs/architecture.md` — the hermetic-side architecture note
  documenting the same layer.

Copied into this repo for convenience:

- [`reference/hermes-skills/subagent-delegation/SKILL.md`](reference/hermes-skills/subagent-delegation/SKILL.md)
  — Hermes's doctrinal skill for delegating to CLI agents
  (tool selection, env sandboxing, prompt management, process
  lifecycle). This is the policy layer that hermes-delegate
  implements in Go. Read this to understand the *intent*
  behind the binary's design decisions.
- [`reference/hermes-skills/subagent-driven-development/SKILL.md`](reference/hermes-skills/subagent-driven-development/SKILL.md)
  — Hermes's workflow for executing plans with delegated
  tasks + two-stage review (spec compliance then code
  quality). More peripheral, but the two-stage review pattern
  is directly relevant to Hubert's reviewer agent.

Both skills originally live in the hermes-agent pod's PVC at
`/opt/data/skills/software-development/`; the copies here are
snapshots. If you need the live versions (they may be updated
out-of-band), `kubectl exec -n hermes deploy/hermes-hermes-agent
-c hermes-agent -- tar -C /opt/data/skills/software-development
-cf - <skill-name> | tar xf -` retrieves them.

## What ports cleanly

### Job submission logic (`docker/tools/hermes-delegate/k8s.go`)

A ~400-line file that:

- Renders a `batch/v1` Job + `v1` ConfigMap via Go
  `text/template` from a struct. The template is embedded in
  the binary; no external template files to ship.
- Submits via `kubectl apply -f -` over stdin (subprocess,
  not client-go — see "why kubectl shell-out" below).
- Waits for the Job's pod to exit Pending, then streams logs
  with `kubectl logs -f --all-containers=true job/<name>`.
- Installs a SIGINT/SIGTERM handler that does a best-effort
  cascade delete of Job + ConfigMap before exiting. Cleanup
  also runs on normal exit via `defer`.
- Supports `-detach` mode that submits the Job and prints the
  job name immediately, leaving TTL-based cleanup
  (`ttlSecondsAfterFinished: 600`) to handle the rest.

This file is essentially Hubert's "execution Job submitter" in
everything but variable naming. Porting it means:

- Rename `hermes-agent/delegated` → `hubert/execution` (or
  whatever label Hubert wants).
- Replace the env-var contract (`HERMETIC_IMAGE`,
  `HERMETIC_DELEGATE_SA`, `HERMETIC_SECRET_NAME`,
  `HERMETIC_CONTEXT_BUCKET`) with Hubert equivalents.
- Replace the prompt-file mount (`/etc/hermes-delegate/prompt`)
  with Hubert's equivalent — likely just the issue number +
  context, passed as env or mounted as a ConfigMap the same
  way.
- Drop the GCS Fuse mount block. Hubert's fresh-clone-per-run
  model means there's nothing to share across Jobs at the
  filesystem level, so the three `ctx-claude` / `ctx-gemini` /
  `ctx-opencode` volumes are dead weight.

### Resource tiers

Defined at the top of `k8s.go` as a map. Current values:

| Tier   | CPU req/lim | Memory req/lim | Deadline |
|--------|-------------|----------------|----------|
| small  | 500m / 1    | 1Gi / 2Gi      | 1h       |
| medium | 1 / 2       | 4Gi / 8Gi      | 2h       |
| large  | 2 / 4       | 8Gi / 16Gi     | 4h       |
| xlarge | 4 / 8       | 16Gi / 32Gi    | 6h       |

xlarge fits a full node's worth of resources on the cluster's
`ek-standard-16` instance type. The 6h deadline is the upper
bound on reasonable single-task work; Hubert should reuse it
until a concrete reason to change appears. For a reviewer Job,
small is almost certainly enough; executor Jobs will need at
least medium.

### Admission controls

`charts/hermes-agent/templates/delegate-admission-policy.yaml`
is a `ValidatingAdmissionPolicy` + `ValidatingAdmissionPolicyBinding`
pair. CEL validations reject any Job created by the agent SA
that doesn't match the delegate template:

- must carry the delegated-label (`hermes-agent/delegated=true`
  today, rename for Hubert);
- must run under the agent SA;
- image must start with the configured image-prefix;
- memory limit must be set and ≤ `maxMemoryBytes`;
- `activeDeadlineSeconds` must be set and ≤ `maxActiveDeadlineSeconds`;
- `backoffLimit` ≤ 1;
- no `hostNetwork`, no `hostPID`, no privileged containers.

`validationActions: [Deny]` — rejected at admission, not just
audited. Scoped to a single namespace via `namespaceSelector`
on the Binding.

This file is 90% template rename work away from being Hubert's
admission policy. Keep the ceilings configurable in values.yaml;
the defaults (32Gi memory, 86400s deadline) are reasonable.

### RBAC (`charts/hermes-agent/templates/rbac.yaml`)

Namespaced `Role` + `RoleBinding` granting the agent SA:

- `create, get, list, watch, delete, patch` on `jobs.batch`
- `create, get, list, delete, patch` on `configmaps`
- `get, list, watch` on `pods`
- `get` on `pods/log`

Intentionally **not** a `ClusterRole`. Each deployment is its
own blast radius. The Hubert equivalent should preserve this
pattern: one namespace, one role, no cluster-scoped grants.

### Workload Identity setup (`scripts/setup-gcs-fsq.sh`)

Bash, idempotent, creates:

- Two GCP service accounts (main + fsq sync; Hubert would
  likely just need one — the executor SA that holds whatever
  GCP credentials the Jobs need).
- A third, separate, read-only GCP SA with an HMAC key for
  DuckDB's httpfs extension, which has no Workload Identity
  support. Hubert probably doesn't need this.
- Workload Identity bindings between K8s SAs and GCP SAs.
- Writes identifiers to GitHub Actions repo secrets via `gh`.

Pattern to port: the `bind_wi` and `ensure_sa` bash functions
are clean and small. The HMAC fallback is only needed if Hubert
touches services that don't speak Workload Identity.

## What does not port

### hermes-delegate agent layer

Files `agent.go`, `prompt.go`, `sandbox.go` are hermetic-
specific glue for invoking `claude -p` / `gemini -p` /
`opencode run` as child processes with env scrubbing and
output collection. Hubert's execution agent is a different
shape (Claude Code runs inside the Job and interacts with
GitHub directly; there's no "wrap the CLI and collect stdout"
layer). Don't port these.

### GCS Fuse context volumes

Hermetic mounts three subpaths of a single GCS bucket
(`forklore-hermes-context/claude`, `/gemini`, `/opencode`)
into every delegated Job so that session state, API
credentials cache, and tool config can be shared across
invocations. This exists because hermes's CLI agents expect
to find persistent `~/.claude`, `~/.gemini`, `~/.config/opencode`
directories.

Hubert doesn't have an analogous requirement. Each execution
agent is a fresh Claude Code run with no cross-run state;
cloning fresh and pushing a branch is the only persistence.
**Skip the GCS Fuse layer entirely.** It adds a sidecar
container, a CSI driver dependency, mount latency, and a CEL
complication to the admission policy, and buys Hubert nothing.

### Hermetic's secret shape

Hermes keeps a large Kubernetes Secret with Discord tokens,
multiple LLM provider keys, Honcho config, OAuth tokens, etc.,
and mounts the whole thing via `envFrom` on both the main pod
and delegated Jobs. Hubert's secret is much smaller — one
Anthropic API key, one GitHub PAT for `hubert-bot`, and
whatever infrastructure bits (GCS bucket names, project IDs)
need to ride along.

## Architectural decisions worth not relitigating

Things we decided and wrote code against in hermetic. Unless
Hubert has a specific reason to differ, keep these.

### `emptyDir` per Job, not shared PVC

Each Job's `/opt/data` (or equivalent) is a fresh `emptyDir`
volume with an 8Gi size limit, isolated from every other Job.
This was a deliberate choice:

- RWO-PVC sharing doesn't work (one pod at a time).
- GCS Fuse for source code is genuinely bad — `git status`
  takes seconds, renames are expensive, no atomic writes.
- Filestore (RWX NFS) costs ~$50/mo minimum tier and wasn't
  worth it.

The correct answer is "each Job clones fresh and pushes a
branch," which is exactly Hubert's model. Don't add shared
storage.

### Prompt delivery via per-Job ConfigMap with `binaryData`

The prompt goes into a ConfigMap named `<job>-prompt`, as a
base64-encoded `binaryData.prompt` entry, mounted at a known
path. This avoids env-var size limits, avoids arg-list limits,
and keeps the ConfigMap atomically cleanable alongside the Job.

For Hubert, the "prompt" might be smaller (an issue number +
pointer to the repo) and could live in env vars instead, but
the ConfigMap pattern is worth keeping in mind for when you
want to ship a full orchestrator snapshot into an executor
Job — those can get large.

### kubectl shell-out beats client-go at this size

hermes-delegate ships as a 5.2MB static binary. The same code
with `k8s.io/client-go` is closer to 30MB. The only K8s
operations the binary needs are `apply -f -`, `get pods`,
`logs -f`, and `delete`. client-go is heavy for that coverage
ratio.

Hubert faces the same tradeoff. The binary will live in a
container image so size matters less, but the dependency
weight of client-go is non-trivial for glue code of this
size. Stick with `os/exec` + `kubectl` until there's a
specific reason not to.

### `ValidatingAdmissionPolicy` over Kyverno

Kyverno is a real dependency — operator, CRDs, version skew,
upgrade mechanics. VAP is built into K8s 1.30+, has CEL
expressions sufficient for this policy shape, and requires
zero extra install.

Use VAP until you have a specific need Kyverno solves that
VAP doesn't.

### Container name matters for log querying

`kubectl logs --all-containers=true job/<name>` merges the
GCS Fuse sidecar's mount output with the main container's
stdout. The sidecar is *loud* — it logs every mount step,
every config flag, every auth check. If you need to read the
agent's output cleanly, name the main container something
specific (`execution`, `reviewer`) and use `kubectl logs -c
<name>`.

For Hubert, the sidecar will be gone (no GCS Fuse), so this
mostly doesn't apply — but name your containers intentionally
anyway; it makes debugging cleaner when you get to the pod
level.

## Open threads

Things we didn't fully resolve that may come up again:

### Empty stdout from opencode under `claude-delegate -k8s`

The end-to-end smoke test run on 2026-04-18 submitted a Job
that completed exit 0 and produced no logs from its main
container. The prompt was `"Say only the word hello, nothing
else."` with `-m openrouter/anthropic/claude-haiku-4-5`. Job
succeeded, cleanup ran, but nothing came back on stdout.

Unclear whether opencode's default output goes to a log file
rather than stdout, or whether the `-f prompt-file` path
silently no-ops on short-seeming input, or something else.

If Hubert reuses opencode as an execution layer, this mystery
needs debugging before "exit 0" is trustworthy as a success
signal. Claude Code (the `claude` CLI, not opencode) is
probably the better default for Hubert's execution agent
anyway — it's the tool the orchestrator/execution/reviewer
prompts were written against, and it has an established,
well-tested `--print` mode.

### Default `--dangerously-skip-permissions` or equivalent

All three CLIs (claude, gemini, opencode) need some variant of
"don't prompt for tool approval" when run non-interactively.
hermetic passes this explicitly in `agent.go`. Hubert's
execution agent will need the same pattern — either pass the
flag, or find whichever non-interactive mode doesn't require
it.

For Claude Code specifically: `claude --print` is the
intended non-interactive mode and doesn't need the flag. For
write-mode tools (Bash, Edit, Write), you'll still need to
configure tool allowlist via settings.json or `--allowed-tools`.

### GH Actions deploy path

hermetic's `.github/workflows/docker-image.yml` builds on
push to main, pushes to `ghcr.io/nemequ/hermetic`, and runs
`helm upgrade --install` against the cluster using a service
account key stored in `GCP_SA_KEY`. Hubert will want the same
shape. The cluster's Workload Identity is configured; the GH
Actions SA (`hermetic-gha-deploy@forklore-491503`) has the
permissions to do the deploy.

If Hubert's image goes to the same `ghcr.io/nemequ` org and
deploys to the same cluster, the existing CI setup mostly
generalizes — just a new workflow file with the Hubert image
ref and Helm chart path.

## Current cluster state

For reference, the state of `ekdromos` at handoff time:

- GKE 1.34, `us-central1`, 2 nodes of `ek-standard-16`.
- GCS Fuse CSI driver enabled.
- Workload Identity enabled, pool
  `forklore-491503.svc.id.goog`.
- Namespace `hermes` exists with the hermes-agent deployment
  running.
- GCP SAs: `hermes-agent`, `hermes-fsq-sync`,
  `hermes-fsq-reader`, `hermetic-gha-deploy`.
- Context bucket: `gs://forklore-hermes-context` (GCS Fuse-
  mounted by hermes pod). Hubert likely doesn't need this.
- FSQ data bucket: `gs://forklore-foursquare-spaces`. Not
  Hubert's concern.

Whether Hubert shares the cluster or gets its own namespace
(`hubert`?) is a v1 decision. Sharing is cheaper and the
namespaced RBAC already isolates workloads; separate clusters
would only matter for blast-radius isolation and nothing
about the threat model seems to justify the cost.

## Starting point for v1 implementation

A suggested first move for the next agent:

1. Read [`ARCHITECTURE.md`](ARCHITECTURE.md) and
   [`PLAN-v1.md`](PLAN-v1.md) to load context on Hubert's
   own design.
2. Read `../hermetic/docker/tools/hermes-delegate/k8s.go` to
   see the portable submission logic.
3. Read `../hermetic/charts/hermes-agent/templates/` for the
   RBAC, admission policy, and SA manifests.
4. Write the three Go binaries defined in
   [`PLAN-v1.md`](PLAN-v1.md): `cmd/hubert-runner` (in-Job
   LLM invocation + lock/heartbeat/escalation),
   `cmd/hubert-dispatch` (Job submitter, ported from
   `k8s.go`), and `cmd/hubert-snap` (GHA-side per-repo
   snapshot builder).
5. Build a reference container image satisfying
   [`IMAGE-CONTRACT.md`](IMAGE-CONTRACT.md) — the hermetic
   Dockerfile is the starting point; add `hubert-runner` to
   `$PATH` and drop anything hermetic-specific that isn't
   needed.

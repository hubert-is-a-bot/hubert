# Hubert GHA workflow templates

These are the workflows a watched repo needs. Copy them into
`.github/workflows/` and commit. No generation step; this is
a deliberate documented contract so human operators can read,
diff, and adjust.

| File | Purpose |
|------|---------|
| [`hubert-orchestrator.yml`](hubert-orchestrator.yml) | Runs on every relevant event + every 10 min; builds a snapshot, runs the orchestrator prompt against `claude`, and hands the resulting action list to `hubert-dispatch`. |
| [`hubert-exec.yml`](hubert-exec.yml) | The execution / reviewer workload. Triggered by `hubert-dispatch` via `workflow_dispatch` (GHA target). Runs `hubert-runner` against one issue or PR. |
| [`hubert-ci.yml`](hubert-ci.yml) | Minimal CI the reviewer agent trusts as ground truth. Single stage (build + test + lint). Edit the three commands to match your project (or the values in `.hubert/README.md`). |

## Execution target: GHA (default) vs. K8s

`hubert-dispatch` supports two execution targets:

- **`gha` (default).** Each `dispatch-execution` /
  `dispatch-reviewer` action triggers `hubert-exec.yml` on
  the same repo via `gh workflow run`. Nothing outside
  GitHub Actions is required. Tier sizing is ignored (GHA
  runners are one size); `activeDeadlineSeconds` is
  approximated by the exec workflow's `timeout-minutes`.
- **`k8s`.** Each dispatch action renders a Kubernetes Job
  and applies it via `kubectl`. Tier sizing (CPU/memory/
  deadline) is honored. Requires cluster setup; see the
  section below.

Select the target by setting the `HUBERT_TARGET` repo variable
to `gha` or `k8s` (unset ≡ `gha`).

## Install checklist

1. Copy `hubert-orchestrator.yml` + `hubert-exec.yml` +
   `hubert-ci.yml` into `.github/workflows/` on your default
   branch.
2. In the repo's **Secrets and variables → Actions**, set:
   - **Secrets (always):** `HUBERT_GH_TOKEN`, `HUBERT_LLM_KEY`.
   - **Secrets (k8s target only):** `HUBERT_KUBECONFIG`.
   - **Variables (optional):**
     - `HUBERT_LLM_ENV` — env var name to receive
       `HUBERT_LLM_KEY`. Default `OPENROUTER_API_KEY`. Set to
       `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` to match your
       provider.
     - `HUBERT_ORCH_AGENT` — CLI used for the orchestrator
       pass. Default `opencode`; also `claude` or `gemini`.
     - `HUBERT_ORCH_MODEL` — model identifier passed to that
       CLI. Default
       `openrouter/deepseek/deepseek-chat-v3.1:free`.
     - `HUBERT_TARGET` — execution target. Default `gha`. For
       `k8s` also set `HUBERT_IMAGE` (runner image ref) and
       `HUBERT_NAMESPACE` (default `hubert`).
3. Adjust the `build` / `test` / `lint` steps in
   `hubert-ci.yml` to match your project, or let
   `.hubert/README.md` override them.
4. Edit `.hubert/README.md` at the repo root — see the root
   `.hubert/README.md.example` in the Hubert repo.
5. Ensure `hubert-is-a-bot` is a collaborator on the repo
   with the permissions your
   [branch protection](../../PLAN.md#410-branch-protection-two-identity-details)
   allows.

## K8s target prerequisite: runner Secret

When `HUBERT_TARGET=k8s`, the dispatched runner Jobs expect a
Kubernetes Secret named `hubert-runner-secrets` in the
namespace they land in (default `hubert`). The Secret must
carry:

| Key | Purpose |
|-----|---------|
| `HUBERT_GH_TOKEN` | PAT used by the runner to configure git auth and by `gh pr create` inside the pod. Same scope as the GHA secret. |
| `OPENROUTER_API_KEY` | Consumed by `opencode run` inside the pod when routing to OpenRouter models (the default). |
| `ANTHROPIC_API_KEY` | Required when the chosen agent is `claude` or when opencode routes to an Anthropic model. |
| `GEMINI_API_KEY` | Required when the chosen agent is `gemini` or when opencode routes to a Google model. |

Create it with:

```
kubectl -n hubert create secret generic hubert-runner-secrets \
  --from-literal=HUBERT_GH_TOKEN=… \
  --from-literal=ANTHROPIC_API_KEY=…
```

The Job template mounts it via `envFrom: secretRef` so every
key becomes an env var inside the pod.

See [PLAN.md §6 Task 5](../../PLAN.md) for the full spec.

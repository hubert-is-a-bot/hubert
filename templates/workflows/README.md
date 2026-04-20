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
2. Register a GitHub App (one-time, across all repos Hubert
   watches). At github.com/settings/apps → *New GitHub App*:
   - Uncheck **Webhook → Active**.
   - **Repository permissions:** Contents RW, Issues RW, Pull
     requests RW, Workflows RW, Metadata R (default), Checks R.
     (The orchestrator and exec workflows narrow per-run via
     `actions/create-github-app-token` — these are the
     maximums the App could ever grant.)
   - Create, then **Generate a private key** → download the
     `.pem`.
   - **Install** the App on every repo Hubert should watch.
3. In each watched repo's **Secrets and variables → Actions**,
   set:
   - **Always:**
     - `HUBERT_APP_ID` — numeric App ID from the App's settings
       page.
     - `HUBERT_APP_PRIVATE_KEY` — full contents of the `.pem`
       file you downloaded in step 2. Workflows mint ~1-hour
       installation tokens from this key per run; the key
       itself never leaves GitHub's secret store.
     - `OPENROUTER_API_KEY`.
   - **Additional provider keys (optional):** passed through to
     exec runs verbatim; set whichever match the backends that
     orchestrator actions may route to — `ANTHROPIC_API_KEY`,
     `GEMINI_API_KEY`, `OPENAI_API_KEY`, `GROQ_API_KEY`,
     `DEEPSEEK_API_KEY`, `CEREBRAS_API_KEY`, `XAI_API_KEY`,
     `MISTRAL_API_KEY`. Unset secrets are ignored.
   - **k8s target only:** `HUBERT_KUBECONFIG`.
   - **Variables (optional):**
     - `HUBERT_ORCH_MODEL` — model identifier for the
       orchestrator pass (default: `z-ai/glm-4.5-air:free`).
     - `HUBERT_ORCH_API_BASE` — OpenAI-compatible base URL for
       the orchestrator pass (default:
       `https://openrouter.ai/api/v1`). Override to target a
       different OpenAI-API-compatible provider.
     - `HUBERT_TARGET` — execution target. Default `gha`. For
       `k8s` also set `HUBERT_IMAGE` (runner image ref) and
       `HUBERT_NAMESPACE` (default `hubert`).
     - `HUBERT_MERGE_STYLE` — `squash` (default), `merge`, or
       `rebase`. Used by the verdict-apply step when the
       reviewer's verdict is `approve`.
4. Adjust the `build` / `test` / `lint` steps in
   `hubert-ci.yml` to match your project, or let
   `.hubert/README.md` override them.
5. Edit `.hubert/README.md` at the repo root — see the root
   `.hubert/README.md.example` in the Hubert repo.

## Trust boundary: the LLM cannot merge

The reviewer LLM's installation token is minted per run with
`pull-requests:read` only — it physically cannot call
`gh pr review --approve` or `gh pr merge` (the API returns
403). The reviewer's job is to emit a structured verdict
comment starting with `🤖 hubert-verdict <run_id> <kind>`.
A deterministic follow-up step in `hubert-exec.yml` parses
that comment and — using the workflow's own `GITHUB_TOKEN`
(whose identity, `github-actions[bot]`, is different from the
exec App's installation identity, so same-author approval
block doesn't fire) — performs the actual approve+merge,
label-flip, or escalation. Even a jailbroken reviewer LLM
cannot force a merge.

## K8s target prerequisite: runner Secret

When `HUBERT_TARGET=k8s`, the dispatched runner Jobs expect a
Kubernetes Secret named `hubert-runner-secrets` in the
namespace they land in (default `hubert`). The Secret must
carry:

| Key | Purpose |
|-----|---------|
| `HUBERT_GH_TOKEN` | Short-lived installation token (~1 hour). The orchestrator workflow mints it via `actions/create-github-app-token` and injects it into the Job spec at dispatch time — it is **not** a standing long-lived value. Used by the runner to configure git auth and by `gh pr create` inside the pod. |
| `OPENROUTER_API_KEY` | Consumed by `opencode run` inside the pod when routing to OpenRouter models (the default). |
| `ANTHROPIC_API_KEY` | Required when the chosen agent is `claude` or when opencode routes to an Anthropic model. |
| `GEMINI_API_KEY` | Required when the chosen agent is `gemini` or when opencode routes to a Google model. |

Because `HUBERT_GH_TOKEN` is minted per dispatch, the K8s
secret is created *by the dispatch code* per-run, not by an
operator. Operator-created baseline secrets are only the
provider keys (`OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`,
etc.):

```
kubectl -n hubert create secret generic hubert-runner-secrets \
  --from-literal=OPENROUTER_API_KEY=… \
  --from-literal=ANTHROPIC_API_KEY=…
```

The Job template mounts it via `envFrom: secretRef` so every
key becomes an env var inside the pod.

See [PLAN.md §6 Task 5](../../PLAN.md) for the full spec.

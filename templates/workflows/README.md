# Hubert GHA workflow templates

These are the two workflows a watched repo needs. Copy them
into `.github/workflows/` and commit. No generation step; this
is a deliberate documented contract so human operators can
read, diff, and adjust.

| File | Purpose |
|------|---------|
| [`hubert-orchestrator.yml`](hubert-orchestrator.yml) | Runs on every relevant event + every 10 min; builds a snapshot, runs the orchestrator prompt against `claude`, and hands the resulting action list to `hubert-dispatch`. |
| [`hubert-ci.yml`](hubert-ci.yml) | Minimal CI the reviewer agent trusts as ground truth. Single stage (build + test + lint). Edit the three commands to match your project (or the values in `.hubert/README.md`). |

## Install checklist

1. Copy both files into `.github/workflows/` on your default branch.
2. In the repo's **Secrets and variables → Actions**, set:
   - **Secrets:** `HUBERT_GH_TOKEN`, `HUBERT_KUBECONFIG`, `HUBERT_ANTHROPIC_KEY`.
   - **Variables:** `HUBERT_IMAGE` (runner image ref), `HUBERT_NAMESPACE` (default `hubert`).
3. Adjust the `build` / `test` / `lint` steps in `hubert-ci.yml` to match your project, or let `.hubert/README.md` override them.
4. Edit `.hubert/README.md` at the repo root — see the root `.hubert/README.md.example` in the Hubert repo for the full shape.
5. Ensure `hubert-is-a-bot` is a collaborator on the repo with the permissions your [branch protection](../../PLAN.md#410-branch-protection-two-identity-details) allows.

See [PLAN.md §6 Task 5](../../PLAN.md) for the full spec.

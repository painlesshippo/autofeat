---
name: autofeat
description: "Manage disposable Git worktree sessions for concurrent AI-agent feature development. Use when creating, opening, inspecting, syncing (rebasing onto base), templating, removing, or tearing down autofeat feature sessions, or recording the artifacts (builds, work items, pull requests, Azure resources) a session produced, including sessions built from Azure DevOps builds or opened in Dev Containers."
compatibility: "Requires the autofeat CLI and Git on PATH."
---

# Autofeat

A feature session groups one worktree or clone per repository under
`<workspace_base_dir>/<flattened-feature>/`. The feature name is both the
session id and the Git branch name in every repository.

## Commands

| Intent | Command |
| --- | --- |
| Create session / add current repo | `autofeat new FEATURE [--ref REF]` |
| Add another local repo or a remote | `autofeat new FEATURE (PATH \| URL)` (auto-detected) |
| Create from template | `autofeat new FEATURE --template NAME` |
| Create from Azure DevOps build | `autofeat new FEATURE --azure-build (URL \| ID --azure-org ORG --azure-project PROJ)` |
| List sessions (offline) | `autofeat list` |
| Inspect repo health (offline) | `autofeat status [SELECTOR...]` |
| Open in editor | `autofeat open SELECTOR...` |
| Open in Dev Container | `autofeat open SELECTOR... --devcontainer PATH/devcontainer.json` |
| Open interactive agent | `autofeat open SELECTOR... --copilot` |
| Fetch and rebase onto base (rolls back on conflict) | `autofeat sync SELECTOR...` |
| Sync, but stop at a conflict to resolve it | `autofeat sync SELECTOR... --keep-conflicts` |
| Abort rebases left in progress | `autofeat sync SELECTOR... --cancel` |
| Detach one repo | `autofeat remove FEATURE (REPO \| PATH \| URL)` |
| Delete sessions | `autofeat teardown SELECTOR...` |
| Record an artifact you created | `autofeat artifact add FEATURE REF [--type TYPE] [--repo REPO]` |
| List / forget artifacts | `autofeat artifact list FEATURE [--json]`, `artifact remove FEATURE REF [--type TYPE] [--repo REPO]` |
| Templates | `autofeat template save NAME --from FEATURE`, `template list`, `template show NAME` |
| Edit config/hooks | `autofeat config` |

`SELECTOR` is an exact name, several names, or a quoted wildcard
(`"feature/*"`, `"*"`). `remove` takes one exact feature only.

## Rules

1. Discover first: `autofeat list`, then `autofeat status FEATURE`. Both are
   offline and read-only.
2. Quote wildcard selectors.
3. Mutating commands: `new`, `sync`, `remove`, `teardown`. State what will change first.
4. Never pass `--force` without explicit user authorization to discard
   changes. "Remove" or "tear down" alone is not authorization.
5. Surface autofeat's warnings verbatim (dirty worktrees, unpushed commits in
   remote clones, conflicting files, sync rollbacks, hook failures).
6. Plain `sync` attempts a session rollback on rebase failure. Check the
   rollback diagnostic before assuming recovery is complete. Use
   `--keep-conflicts` only when you or the user will resolve the conflict
   right away.
7. Record every external artifact you create for a session (pipeline run, work
   item, pull request, Azure resource, other URL) with `autofeat artifact add`.
   Pass the URL or resource id; use `--type` only for plain ids.
8. Do not run interactive commands: `dash` needs a TTY, and `open --copilot`
   blocks on an interactive agent session. Run them only when the user asks.
9. Locks protect state updates, worktree registration, and
   `new`/`remove`/`teardown` for the same feature directory. Do not overlap
   `sync` or `sync --cancel` with other mutations of the same worktree.
   Run template saves one at a time per home directory.
10. Never delete lock files in `$HOME/.autofeat`, and do not hand-edit
    `$HOME/.autofeat/*.json` unless the user asks; prefer CLI commands.

## References

- [Session Lifecycle](./references/session-lifecycle.md): creation sources,
  base refs, Azure DevOps, `list`/`status` output, opening, sync, artifacts.
- [Safety And Recovery](./references/safety-and-recovery.md): remove,
  teardown, `--force`, sync rollback and conflicts, hook failures.
- [Templates And Configuration](./references/templates-and-config.md):
  templates, `config.json` fields, hooks, state and lock files.

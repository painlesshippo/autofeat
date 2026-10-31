---
name: autofeat
description: "Manage disposable Git worktree sessions for concurrent AI-agent feature development. Use when creating, opening, running, inspecting, syncing, templating, removing, or tearing down autofeat feature sessions, including sessions built from Azure DevOps builds or opened in Dev Containers."
---

# Autofeat

A feature session groups one worktree or clone per repository under
`<workspace_base_dir>/<flattened-feature>/`. The feature name is both the
session id and the Git branch name in every repository.

## Commands

| Intent | Command |
| --- | --- |
| Create session / add current repo | `autofeat new FEATURE [--ref REF]` |
| Add another local repo or a remote | `autofeat new FEATURE --local PATH` / `--remote URL` |
| Create from template | `autofeat new FEATURE --template NAME` |
| Create from Azure DevOps build | `autofeat new FEATURE --azure-build (URL \| ID --azure-org ORG --azure-project PROJ)` |
| List sessions (offline) | `autofeat list` |
| Inspect repo health (offline) | `autofeat status [SELECTOR...]` |
| Open in editor | `autofeat open SELECTOR...` |
| Open in Dev Container | `autofeat open SELECTOR... --devcontainer PATH/devcontainer.json` |
| Open interactive agent | `autofeat open SELECTOR... --copilot` |
| Fetch and rebase onto base | `autofeat sync SELECTOR...` |
| Abort a stuck sync rebase | `autofeat sync SELECTOR... --cancel` |
| Detach one repo | `autofeat remove FEATURE (REPO \| --local PATH \| --remote URL)` |
| Delete sessions | `autofeat teardown SELECTOR...` |
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
   remote clones, rebase conflicts, hook failures).
6. Do not hand-edit `$HOME/.autofeat/*.json` unless the user asks; prefer CLI
   commands.

## References

- [Session Lifecycle](./references/session-lifecycle.md): creation sources,
  base refs, Azure DevOps, opening, running, `list`/`status` output, sync.
- [Safety And Recovery](./references/safety-and-recovery.md): remove,
  teardown, `--force`, rebase conflicts, hook failures.
- [Templates And Configuration](./references/templates-and-config.md):
  templates, `config.json` fields, hooks, state files.

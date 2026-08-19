---
name: autofeat
description: "Manage disposable Git worktree sessions for concurrent AI-agent feature development. Use when creating, opening, running, inspecting, syncing, templating, removing, or tearing down autofeat feature sessions."
---

# Autofeat

Use `autofeat` to manage a feature session containing one or more repository
worktrees. Treat the feature name as both the session identifier and the Git
branch name.

## Route The Request

| User intent | Command |
| --- | --- |
| Create a session or attach a repository | `autofeat new` |
| Open a session in an editor or interactive agent | `autofeat open` |
| append an objective and start the headless agent | `autofeat run` |
| Discover or inspect sessions | `autofeat list`, `autofeat status` |
| Rebase clean worktrees onto their base references | `autofeat sync` |
| Detach one repository | `autofeat remove` |
| Delete an entire session | `autofeat teardown` |
| Save or instantiate a repository group | `autofeat template`, `autofeat new --template` |
| Edit global settings and hooks | `autofeat config` |

Read [Session Lifecycle](./references/session-lifecycle.md) for creation,
selectors, opening, running, status, and synchronization.

Read [Safety And Recovery](./references/safety-and-recovery.md) before removal,
teardown, forced cleanup, or rebase recovery.

Read [Templates And Configuration](./references/templates-and-config.md) when
reusing repository groups or changing editor, agent, workspace, and hook
settings.

## Operating Rules

1. Confirm `autofeat` is available before relying on it.
2. Use `autofeat list` to discover session names and `autofeat status` to
   inspect repository health. Both are offline.
3. Quote selectors containing `*` so the shell does not expand them.
4. Prefer explicit `--local`, `--remote`, and `--ref` arguments when context is
   ambiguous.
5. Explain mutations before running them. `new`, `run` from inside a Git
   repository, `sync`, `remove`, and `teardown` can modify local state.
6. Never add `--force` unless the user explicitly authorizes discarding local
   changes. A general request to remove or tear down a session is not force
   authorization.
7. Report the command result and any recovery instructions printed by
   `autofeat`; do not hide warnings about dirty worktrees, rebases, or unpushed
   commits.

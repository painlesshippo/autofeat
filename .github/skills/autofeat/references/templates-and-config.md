# Templates And Configuration

## Reuse Repository Groups

Save the ordered local and remote repository sources from an active session:

```sh
autofeat template save full-stack --from feature/name
```

Discover and inspect templates:

```sh
autofeat template list
autofeat template show full-stack
```

Create a new session from every repository in a template:

```sh
autofeat new feature/other --template full-stack
```

Template creation requires a feature name that has no existing session. It
retains source paths and URLs in repository order, but not generated worktree
paths, base references, or the original feature name. Autofeat validates all
sources before creation and rolls back repositories created for the new
session if a later addition fails.

Normal base-reference selection and `post-add` hooks apply to every template
repository. `--template` cannot be combined with `--ref`, `--local`, or
`--remote`.

## Edit Global Configuration

Open `$HOME/.autofeat/config.json` in the configured editor with:

```sh
autofeat config
```

The main fields are:

| Field | Purpose |
| --- | --- |
| `workspace_base_dir` | Parent directory for feature worktrees and workspace files |
| `editor_cmd` | Executable used by `autofeat open` and `autofeat config` |
| `headless_cmd` | Interactive or headless agent executable used by `open --copilot` and `run` |
| `hooks` | Commands run at supported lifecycle events |

Preserve `schema_version` and existing fields when editing. Unknown fields are
rejected. Set `hooks` to an empty array to disable all hooks.

The default `post-add` hook runs `mise trust && mise install` only when the new
repository contains `mise.toml` or `.mise.toml`. Hook commands run through
`sh`, with the new worktree or clone as their working directory.

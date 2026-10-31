# Templates And Configuration

## Templates

```sh
autofeat template save full-stack --from FEATURE
autofeat template list
autofeat template show full-stack
autofeat new feature/other --template full-stack
```

- Templates store ordered local paths and remote URLs only. They do not store
  worktree paths, base refs, or the feature name.
- All sources are validated before anything is created. A later failure rolls
  back repos already created.
- Normal base-ref selection and `post-add` hooks apply.

## Files Under `$HOME/.autofeat`

| File | Contents |
| --- | --- |
| `config.json` | Global settings (`autofeat config` opens it) |
| `state.json` | Sessions, `default_base_branch`, remembered `repository_base_branches` |
| `templates.json` | Saved templates |

Every file has a `schema_version`, and unknown fields are rejected. Keep the
version and existing fields if you edit one.

## config.json

| Field | Default | Purpose |
| --- | --- | --- |
| `workspace_base_dir` | `~/.autofeat-workspaces` | Parent of feature directories |
| `editor_cmd` | `code` | Used by `open`, `config`, and `open --devcontainer` (must support `--file-uri`) |
| `headless_cmd` | `copilot` | Used by `open --copilot` |
| `hooks` | mise trust/install | Lifecycle commands; `[]` disables all |

Hook shape:

```json
{ "when": "post-add", "if_files": ["mise.toml", ".mise.toml"], "run": "mise trust && mise install" }
```

- `when`: `post-add` (runs in the new worktree/clone) or `post-teardown`.
- `if_files` (optional): run only if the repo contains at least one listed file.
- `run`: executed with `sh`.

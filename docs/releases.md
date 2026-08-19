# Release Implementation

`autofeat` uses a two-stage release process:

1. `mise run release` determines and pushes a semantic version tag from the
   maintainer's machine.
2. The pushed tag starts GitHub Actions, which tests, packages, and publishes
   the GitHub Release.

This separation keeps version selection under explicit maintainer control while
making every published Linux and Windows artifact traceable to a tagged commit.

## Components

| File | Responsibility |
| --- | --- |
| `scripts/release.sh` | Validates the repository, calculates the next version, tests, tags, verifies, and pushes the tag. |
| `scripts/version.sh` | Derives local build metadata from Git tags, branches, and worktree state. |
| `scripts/build.sh` | Builds the local verification binary in `bin/autofeat`. |
| `.github/workflows/release.yml` | Runs the server-side test and publication job for pushed `v*` tags. |
| `.goreleaser.yaml` | Defines release targets, metadata, archive names, checksums, and release notes. |
| `.github/skills/autofeat` | Contains the agent skill bundled with every release archive. |
| `scripts/install-linux.sh` | Downloads and verifies the Linux release archive. |
| `scripts/install-win.ps1` | Downloads and verifies the Windows release archive. |

## Version Selection and Tagging

Releases are created from `master` with a clean worktree and an authenticated
`origin` remote:

```sh
mise run release
```

The release script uses `svu next` and Conventional Commits to calculate the
next `vMAJOR.MINOR.PATCH` tag. Before pushing anything, it performs these
checks:

* The worktree is clean.
* The current branch is `master`.
* The `origin` remote exists and is reachable.
* The calculated tag does not already exist locally.
* `go test -v ./...` passes.

The script then creates an annotated tag, builds `bin/autofeat`, and checks that
`autofeat version` reports the version without the tag's `v` prefix. A build or
version-check failure removes the local tag. A successful check pushes only the
tag to `origin`.

## GitHub Publication

The `Release` workflow runs when GitHub receives any tag matching `v*`. It:

1. Checks out the complete Git history so GoReleaser can inspect tags and
   generate release notes.
2. Installs the Go version declared in `go.mod`.
3. Runs `go test ./...` again so a manually pushed tag cannot bypass tests.
4. Exports the Go toolchain version as `GOVERSION`.
5. Runs GoReleaser `v2.17.0` with `release --clean`.

The workflow grants `contents: write` to the repository-scoped
`GITHUB_TOKEN`. No personal access token or additional repository secret is
required.

## Build Targets and Artifacts

GoReleaser cross-compiles with `CGO_ENABLED=0` for two amd64 targets:

| Target | Release artifact | Executable |
| --- | --- | --- |
| Linux, including WSL | `autofeat_<version>_linux_amd64.tar.gz` | `autofeat` |
| Windows | `autofeat_<version>_windows_amd64.zip` | `autofeat.exe` |

Both archives also contain `skills/autofeat`, including its `SKILL.md` manifest
and progressively loaded references. The installers copy that directory to
`$HOME/.agents/skills/autofeat` independently of the executable installation
directory.

Each GitHub Release also includes `checksums.txt` containing SHA-256 checksums
for both archives. GoReleaser generates release notes from GitHub commit
history.

The binaries are built with `-trimpath`, stripped debug information, and these
linker-injected values:

| Variable | Value |
| --- | --- |
| `main.version` | Semantic version without the `v` prefix. |
| `main.commit` | Full commit hash referenced by the tag. |
| `main.buildDatetime` | UTC GoReleaser build time. |
| `main.goVersion` | Go toolchain version exported by the workflow. |

Users can inspect all four values with `autofeat version`.

## Local Validation

Validate application behavior, installers, and release packaging before
creating a tag:

```sh
mise run test
mise run test-install
mise run release-check
```

`test-install` runs the Linux release installer, the Linux source installer,
and the Windows release installer. It uses `pwsh` when available or native
Windows PowerShell when run from WSL. The release installer tests download the
current GitHub Release and temporarily modify isolated install locations. The
Windows test restores the user `PATH` when it finishes.

`release-check` validates `.goreleaser.yaml` and creates a non-publishing
snapshot in `dist/`. The `dist/` directory is ignored by Git. A successful
snapshot contains one Linux archive, one Windows archive, and `checksums.txt`.
Each archive contains the platform executable and the installable `autofeat`
agent skill.

## Failure Recovery

Failures before the tag is pushed do not publish a release. Fix the problem and
run `mise run release` again. If `git push` fails after the local tag is
created, correct authentication or connectivity and push that existing tag:

```sh
git push origin <tag>
```

If GitHub Actions fails for a valid pushed tag, use GitHub Actions to rerun the
failed job after correcting transient repository settings. Do not move an
already published version tag to a different commit. For a code or release
configuration fix, commit the correction and publish the next semantic version.

After publication, confirm that the GitHub Release contains both archives and
`checksums.txt`, then run:

```sh
mise run test-install
```

#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

mapfile -d '' go_files < <(git ls-files --cached --others --exclude-standard -z -- '*.go')
mapfile -d '' markdown_files < <(git ls-files --cached --others --exclude-standard -z -- '*.md' '*.markdown')
mapfile -d '' shell_files < <(git ls-files --cached --others --exclude-standard -z -- '*.sh' '.githooks/*')
mapfile -d '' toml_files < <(git ls-files --cached --others --exclude-standard -z -- '*.toml')

if ((${#go_files[@]})); then
    gofmt -w "${go_files[@]}"
fi

if ((${#markdown_files[@]})); then
    rumdl fmt "${markdown_files[@]}"
fi

if ((${#shell_files[@]})); then
    shfmt -w -i 4 "${shell_files[@]}"
fi

if ((${#toml_files[@]})); then
    taplo fmt "${toml_files[@]}"
fi

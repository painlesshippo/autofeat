#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

mapfile -d '' go_files < <(git ls-files --cached --others --exclude-standard -z -- '*.go')
mapfile -d '' markdown_files < <(git ls-files --cached --others --exclude-standard -z -- '*.md' '*.markdown')
mapfile -d '' shell_files < <(git ls-files --cached --others --exclude-standard -z -- '*.sh' '.githooks/*')
mapfile -d '' template_files < <(git ls-files --cached --others --exclude-standard -z -- '*.tmpl')
mapfile -d '' toml_files < <(git ls-files --cached --others --exclude-standard -z -- '*.toml')

if ((${#go_files[@]})); then
    printf 'Formatting %d Go files with gofmt...\n' "${#go_files[@]}"
    gofmt -w "${go_files[@]}"
fi

if ((${#markdown_files[@]})); then
    printf 'Formatting %d Markdown files with rumdl...\n' "${#markdown_files[@]}"
    rumdl fmt "${markdown_files[@]}"
fi

if ((${#shell_files[@]})); then
    printf 'Formatting %d shell files with shfmt...\n' "${#shell_files[@]}"
    shfmt -w -i 4 "${shell_files[@]}"
fi

if ((${#template_files[@]})); then
    printf 'Formatting %d template files with gotmplfmt...\n' "${#template_files[@]}"
    gotmplfmt -w "${template_files[@]}"
fi

if ((${#toml_files[@]})); then
    printf 'Formatting %d TOML files with taplo...\n' "${#toml_files[@]}"
    taplo fmt "${toml_files[@]}"
fi

printf 'Formatting complete.\n'

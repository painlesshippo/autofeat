#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

start_time_ms=$(date +%s%3N)

mapfile -d '' go_files < <(git ls-files --cached --others --exclude-standard -z -- '*.go')
mapfile -d '' markdown_files < <(git ls-files --cached --others --exclude-standard -z -- '*.md' '*.markdown')
mapfile -d '' shell_files < <(git ls-files --cached --others --exclude-standard -z -- '*.sh' '.githooks/*')
mapfile -d '' template_files < <(git ls-files --cached --others --exclude-standard -z -- '*.tmpl')
mapfile -d '' toml_files < <(git ls-files --cached --others --exclude-standard -z -- '*.toml')

formatter_pids=()
formatter_names=()

if ((${#go_files[@]})); then
    printf 'Formatting %d Go files with gofmt...\n' "${#go_files[@]}"
    gofmt -w "${go_files[@]}" &
    formatter_pids+=("$!")
    formatter_names+=("gofmt")
fi

if ((${#markdown_files[@]})); then
    printf 'Formatting %d Markdown files with rumdl...\n' "${#markdown_files[@]}"
    rumdl fmt --quiet --no-cache "${markdown_files[@]}" &
    formatter_pids+=("$!")
    formatter_names+=("rumdl")
fi

if ((${#shell_files[@]})); then
    printf 'Formatting %d shell files with shfmt...\n' "${#shell_files[@]}"
    shfmt -w -i 4 "${shell_files[@]}" &
    formatter_pids+=("$!")
    formatter_names+=("shfmt")
fi

if ((${#template_files[@]})); then
    printf 'Formatting %d template files with gotmplfmt...\n' "${#template_files[@]}"
    gotmplfmt -w "${template_files[@]}" &
    formatter_pids+=("$!")
    formatter_names+=("gotmplfmt")
fi

if ((${#toml_files[@]})); then
    printf 'Formatting %d TOML files with taplo...\n' "${#toml_files[@]}"
    RUST_LOG=warn taplo fmt "${toml_files[@]}" &
    formatter_pids+=("$!")
    formatter_names+=("taplo")
fi

format_status=0
for formatter_index in "${!formatter_pids[@]}"; do
    if ! wait "${formatter_pids[$formatter_index]}"; then
        printf '%s failed.\n' "${formatter_names[$formatter_index]}" >&2
        format_status=1
    fi
done

if ((format_status != 0)); then
    exit "$format_status"
fi

end_time_ms=$(date +%s%3N)
printf 'Formatting complete in %dms.\n' "$((end_time_ms - start_time_ms))"

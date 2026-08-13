#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_dir="$(mktemp -d)"
install_dir="$test_dir/bin"
shell_rc="$test_dir/bashrc"

cleanup() {
    rm -rf "$test_dir"
}
trap cleanup EXIT

arguments=()
if [[ -n "${TEST_VERSION:-}" ]]; then
    arguments+=(--version "$TEST_VERSION")
fi

INSTALL_DIR="$install_dir" SHELL_RC="$shell_rc" \
    bash "$script_dir/install-linux.sh" "${arguments[@]}"

binary="$install_dir/autofeat"
if [[ ! -x "$binary" ]]; then
    echo "Expected executable at $binary." >&2
    exit 1
fi

version_output="$("$binary" version)"
grep -Eq '^autofeat [0-9]+\.[0-9]+\.[0-9]+' <<<"$version_output"
grep -Eq '^commit: [0-9a-f]{40}$' <<<"$version_output"
grep -Eq '^built: [0-9]{4}-[0-9]{2}-[0-9]{2}T' <<<"$version_output"
grep -Eq '^go: go[0-9]+\.' <<<"$version_output"

grep -Fqx "export PATH=\"$install_dir:\$PATH\"" "$shell_rc"
grep -Fqx "source <(autofeat completion bash)" "$shell_rc"
if grep -Eq '^alias (af|afl)=' "$shell_rc"; then
    echo "Installer unexpectedly added autofeat aliases." >&2
    exit 1
fi

echo "PASS: install-linux.sh"

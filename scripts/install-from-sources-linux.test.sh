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

INSTALL_DIR="$install_dir" SHELL_RC="$shell_rc" \
    bash "$script_dir/install-from-sources-linux.sh"

binary="$install_dir/autofeat"
if [[ ! -x "$binary" ]]; then
    echo "Expected executable at $binary." >&2
    exit 1
fi

version_output="$("$binary" version)"
if [[ "$(head -n 1 <<<"$version_output")" == "autofeat dev" ]]; then
    echo "Source build retained the default development version." >&2
    exit 1
fi
grep -Eq '^commit: [0-9a-f]{40}$' <<<"$version_output"
grep -Eq '^built: [0-9]{4}-[0-9]{2}-[0-9]{2}T' <<<"$version_output"
grep -Eq '^go: go[0-9]+\.' <<<"$version_output"

grep -Fqx "export PATH=\"$install_dir:\$PATH\"" "$shell_rc"
grep -Fqx "alias af='autofeat'" "$shell_rc"
grep -Fqx "alias afl='autofeat list'" "$shell_rc"
grep -Fqx "source <(autofeat completion bash)" "$shell_rc"

echo "PASS: install-from-sources-linux.sh"

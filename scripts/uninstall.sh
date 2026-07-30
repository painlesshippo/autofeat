#!/usr/bin/env bash

set -euo pipefail

install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""
completion_source="source <(autofeat completion bash)"
alias_af="alias af='autofeat'"
legacy_alias_afr="alias afr='autofeat review'"
alias_afl="alias afl='autofeat list'"

rm -f "$install_dir/autofeat"
rm -f "$HOME/.autofeat/review.html"

if [[ -f "$shell_rc" ]] && grep -Fqx "# Added by autofeat install" "$shell_rc"; then
    temporary_rc="$(mktemp "${shell_rc}.XXXXXX")"
    awk \
        -v marker="# Added by autofeat install" \
        -v path_export="$path_export" \
        -v completion_source="$completion_source" \
        -v alias_af="$alias_af" \
        -v legacy_alias_afr="$legacy_alias_afr" \
        -v alias_afl="$alias_afl" '
        $0 == marker {
            managed_block = 1
            next
        }
        managed_block && ($0 == path_export || $0 == completion_source || $0 == alias_af || $0 == legacy_alias_afr || $0 == alias_afl) {
            next
        }
        managed_block {
            managed_block = 0
        }
        {
            print
        }
    ' "$shell_rc" >"$temporary_rc"
    mv "$temporary_rc" "$shell_rc"
    echo "Removed $install_dir from PATH in $shell_rc"
    echo "Run: source $shell_rc"
fi

echo "Uninstalled $install_dir/autofeat"

#!/usr/bin/env bash

set -euo pipefail

install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""

rm -f "$install_dir/autofeat"

if [[ -f "$shell_rc" ]] && grep -Fqx "# Added by autofeat install" "$shell_rc"; then
    temporary_rc="$(mktemp "${shell_rc}.XXXXXX")"
    awk -v marker="# Added by autofeat install" -v path_export="$path_export" '
        $0 == marker {
            remove_next = 1
            next
        }
        remove_next && $0 == path_export {
            remove_next = 0
            next
        }
        {
            remove_next = 0
            print
        }
    ' "$shell_rc" > "$temporary_rc"
    mv "$temporary_rc" "$shell_rc"
    echo "Removed $install_dir from PATH in $shell_rc"
    echo "Run: source $shell_rc"
fi

echo "Uninstalled $install_dir/autofeat"

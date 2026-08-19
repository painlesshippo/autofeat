#!/usr/bin/env bash

set -euo pipefail

install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
skill_dir="${SKILL_DIR:-$HOME/.agents/skills}"
skill_target="$skill_dir/autofeat"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""
completion_source="source <(autofeat completion bash)"

rm -f "$install_dir/autofeat"
rm -rf "$skill_target"

if [[ -f "$shell_rc" ]] && grep -Fqx "# Added by autofeat install" "$shell_rc"; then
    temporary_rc="$(mktemp "${shell_rc}.XXXXXX")"
    awk \
        -v marker="# Added by autofeat install" \
        -v path_export="$path_export" \
        -v completion_source="$completion_source" '
        $0 == marker {
            managed_block = 1
            next
        }
        managed_block && ($0 == path_export || $0 == completion_source) {
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
echo "Uninstalled $skill_target"

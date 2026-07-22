#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary_path="$project_root/bin/autofeat"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""

mkdir -p "$install_dir"
cp "$binary_path" "$install_dir/autofeat"
chmod +x "$install_dir/autofeat"

case ":$PATH:" in
    *":$install_dir:"*) ;;
    *)
        mkdir -p "$(dirname "$shell_rc")"
        if ! grep -Fqx "$path_export" "$shell_rc" 2>/dev/null; then
            {
                echo
                echo "# Added by autofeat install"
                echo "$path_export"
            } >> "$shell_rc"
        fi
        echo "Added $install_dir to PATH in $shell_rc"
        echo "Run: source $shell_rc"
        ;;
esac

echo "Installed $install_dir/autofeat"

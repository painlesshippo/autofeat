#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary_path="$project_root/bin/autofeat"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""
alias_definitions=(
    "alias af='autofeat'"
    "alias afp='autofeat review'"
    "alias afl='autofeat list'"
)

mkdir -p "$install_dir"
cp "$binary_path" "$install_dir/autofeat"
chmod +x "$install_dir/autofeat"

add_path=false
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) add_path=true ;;
esac

aliases_to_add=()
for alias_definition in "${alias_definitions[@]}"; do
    if ! grep -Fqx "$alias_definition" "$shell_rc" 2>/dev/null; then
        aliases_to_add+=("$alias_definition")
    fi
done

if [[ "$add_path" == true || ${#aliases_to_add[@]} -gt 0 ]]; then
    mkdir -p "$(dirname "$shell_rc")"
    {
        echo
        echo "# Added by autofeat install"
        if [[ "$add_path" == true ]]; then
            echo "$path_export"
        fi
        for alias_definition in "${aliases_to_add[@]}"; do
            echo "$alias_definition"
        done
    } >> "$shell_rc"
fi

if [[ "$add_path" == true ]]; then
    echo "Added $install_dir to PATH in $shell_rc"
fi
if [[ ${#aliases_to_add[@]} -gt 0 ]]; then
    echo "Added autofeat aliases in $shell_rc"
fi
if [[ "$add_path" == true || ${#aliases_to_add[@]} -gt 0 ]]; then
    echo "Run: source $shell_rc"
fi

echo "Installed $install_dir/autofeat"

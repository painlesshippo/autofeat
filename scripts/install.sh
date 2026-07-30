#!/usr/bin/env bash

set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary_path="$project_root/bin/autofeat"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
shell_rc="${SHELL_RC:-$HOME/.bashrc}"
path_export="export PATH=\"$install_dir:\$PATH\""
completion_source="source <(autofeat completion bash)"
legacy_alias_afr="alias afr='autofeat review'"
alias_definitions=(
    "alias af='autofeat'"
    "alias afl='autofeat list'"
)

if [[ -f "$shell_rc" ]] && grep -Fqx "$legacy_alias_afr" "$shell_rc"; then
    temporary_rc="$(mktemp "${shell_rc}.XXXXXX")"
    awk \
        -v marker="# Added by autofeat install" \
        -v path_export="$path_export" \
        -v completion_source="$completion_source" \
        -v alias_af="${alias_definitions[0]}" \
        -v legacy_alias_afr="$legacy_alias_afr" \
        -v alias_afl="${alias_definitions[1]}" '
        $0 == marker {
            managed_block = 1
            print
            next
        }
        managed_block && ($0 == path_export || $0 == completion_source || $0 == alias_af || $0 == legacy_alias_afr || $0 == alias_afl) {
            if ($0 != legacy_alias_afr) {
                print
            }
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
fi

rm -f "$HOME/.autofeat/review.html"

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

add_completion=false
if ! grep -Fqx "$completion_source" "$shell_rc" 2>/dev/null; then
    add_completion=true
fi

if [[ "$add_path" == true || ${#aliases_to_add[@]} -gt 0 || "$add_completion" == true ]]; then
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
        if [[ "$add_completion" == true ]]; then
            echo "$completion_source"
        fi
    } >>"$shell_rc"
fi

if [[ "$add_path" == true ]]; then
    echo "Added $install_dir to PATH in $shell_rc"
fi
if [[ ${#aliases_to_add[@]} -gt 0 ]]; then
    echo "Added autofeat aliases in $shell_rc"
fi
if [[ "$add_completion" == true ]]; then
    echo "Added autofeat Bash completion in $shell_rc"
fi
if [[ "$add_path" == true || ${#aliases_to_add[@]} -gt 0 || "$add_completion" == true ]]; then
    echo "Run: source $shell_rc"
fi

echo "Installed $install_dir/autofeat"

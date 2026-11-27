#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
venv_dir="$project_dir/.venv"
link_path="$HOME/.local/bin/lark"
if [[ -L "$link_path" && "$(readlink "$link_path")" == "$venv_dir/bin/lark" ]]; then
    rm "$link_path"
fi
if [[ -d "$venv_dir" ]]; then
    rm -rf "$venv_dir"
fi
echo "Removed this project's CLI link and virtual environment."

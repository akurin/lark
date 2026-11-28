#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="$project_dir/build/lark"
link_path="$HOME/.local/bin/lark"
if [[ -L "$link_path" && "$(readlink "$link_path")" == "$binary" ]]; then
    rm "$link_path"
fi
app_dir="$HOME/Applications/Lark Helper.app"
marker="$app_dir/Contents/Resources/lark-build-id"
if [[ -f "$marker" && "$(cat "$marker")" == go:"$project_dir":* ]]; then
    rm -rf "$app_dir"
fi
rm -f "$binary"
echo "Removed this project's Go CLI and helper."

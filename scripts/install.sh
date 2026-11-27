#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
venv_dir="$project_dir/.venv"
bin_dir="$HOME/.local/bin"
link_path="$bin_dir/lark"

if [[ ! -x "$venv_dir/bin/python" ]]; then
    if [[ -e "$venv_dir" ]]; then
        echo "Rebuilding unusable virtual environment at $venv_dir" >&2
        rm -rf "$venv_dir"
    fi
    python3 -m venv "$venv_dir"
fi

if ! "$venv_dir/bin/python" -c 'import hid' >/dev/null 2>&1; then
    "$venv_dir/bin/python" -m pip install --disable-pip-version-check 'hidapi>=0.14,<1'
fi
site_dir="$("$venv_dir/bin/python" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
printf '%s\n' "$project_dir/src" > "$site_dir/lark-a2-cli.pth"
printf '#!%s\nfrom lark.cli import main\nraise SystemExit(main())\n' "$venv_dir/bin/python" > "$venv_dir/bin/lark"
chmod +x "$venv_dir/bin/lark"
mkdir -p "$bin_dir"
if [[ -e "$link_path" && ! -L "$link_path" ]]; then
    echo "Refusing to replace existing file: $link_path" >&2
    exit 1
fi
if [[ -L "$link_path" && "$(readlink "$link_path")" != "$venv_dir/bin/lark" ]]; then
    echo "Refusing to replace another lark symlink: $link_path" >&2
    exit 1
fi
ln -sfn "$venv_dir/bin/lark" "$link_path"
echo "Installed: $link_path"
case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "Add $bin_dir to PATH yourself; no shell configuration was changed." >&2 ;;
esac

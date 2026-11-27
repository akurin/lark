#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "Lark Helper can only be built on macOS." >&2
    exit 1
fi
if ! command -v swiftc >/dev/null 2>&1; then
    echo "swiftc is required; install Xcode Command Line Tools with xcode-select --install." >&2
    exit 1
fi

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
venv_dir="$project_dir/.venv"
python_path="$venv_dir/bin/python"
if [[ ! -x "$python_path" || ! -x "$venv_dir/bin/lark" ]]; then
    echo "Run scripts/install.sh first." >&2
    exit 1
fi

apps_dir="$HOME/Applications"
app_dir="$apps_dir/Lark Helper.app"
mkdir -p "$apps_dir"
source_hash="$(shasum -a 256 "$project_dir/scripts/LarkHelper.swift" | cut -d ' ' -f 1)"
build_id="$source_hash:$python_path"
marker="$app_dir/Contents/Resources/lark-build-id"
if [[ -f "$marker" && "$(cat "$marker")" == "$build_id" ]] && codesign --verify --deep --strict "$app_dir" >/dev/null 2>&1; then
    echo "Lark Helper is already installed: $app_dir"
    exit 0
fi
if [[ -e "$app_dir" && ! -f "$marker" ]]; then
    echo "Refusing to replace an unrecognized app: $app_dir" >&2
    exit 1
fi

build_dir="$(mktemp -d "$apps_dir/.lark-helper-build.XXXXXX")"
trap 'rm -rf "$build_dir"' EXIT
new_app="$build_dir/Lark Helper.app"
mkdir -p "$new_app/Contents/MacOS" "$new_app/Contents/Resources"
swiftc -O "$project_dir/scripts/LarkHelper.swift" -o "$new_app/Contents/MacOS/LarkHelper"
"$python_path" - "$new_app/Contents/Info.plist" "$python_path" <<'PY'
import plistlib
import sys

plist = {
    "CFBundleIdentifier": "com.akurin.lark.helper",
    "CFBundleName": "Lark Helper",
    "CFBundleDisplayName": "Lark Helper",
    "CFBundleExecutable": "LarkHelper",
    "CFBundlePackageType": "APPL",
    "CFBundleShortVersionString": "0.1.0",
    "CFBundleVersion": "1",
    "LSMinimumSystemVersion": "13.0",
    "LSUIElement": True,
    "LarkPythonPath": sys.argv[2],
}
with open(sys.argv[1], "wb") as handle:
    plistlib.dump(plist, handle)
PY
printf '%s\n' "$build_id" > "$new_app/Contents/Resources/lark-build-id"
codesign --force --sign - --timestamp=none "$new_app"
codesign --verify --deep --strict "$new_app"

backup_dir="$build_dir/previous.app"
if [[ -e "$app_dir" ]]; then
    mv "$app_dir" "$backup_dir"
fi
if ! mv "$new_app" "$app_dir"; then
    if [[ -e "$backup_dir" ]]; then mv "$backup_dir" "$app_dir"; fi
    echo "Could not install Lark Helper." >&2
    exit 1
fi
echo "Installed: $app_dir"
echo "Run lark doctor to launch the helper. If macOS requests Input Monitoring, grant it to Lark Helper and retry."

#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "Build the Go HID app on macOS (cgo needs the macOS SDK)." >&2
    exit 1
fi
if ! command -v go >/dev/null 2>&1; then
    echo "Go is required; install it from go.dev or Homebrew." >&2
    exit 1
fi
if ! command -v codesign >/dev/null 2>&1; then
    echo "codesign is required; install Xcode Command Line Tools." >&2
    exit 1
fi

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
apps_dir="$HOME/Applications"
app_dir="$apps_dir/Lark Helper.app"
bin_dir="$HOME/.local/bin"
binary="$project_dir/build/lark"
mkdir -p "$apps_dir" "$bin_dir" "$project_dir/build"
link_path="$bin_dir/lark"
if [[ -e "$link_path" && ! -L "$link_path" ]]; then
    echo "Refusing to replace existing file: $link_path" >&2
    exit 1
fi
if [[ -L "$link_path" ]]; then
    old_target="$(readlink "$link_path")"
    if [[ "$old_target" != "$binary" && "$old_target" != "$project_dir/.venv/bin/lark" ]]; then
        echo "Refusing to replace another lark symlink: $link_path" >&2
        exit 1
    fi
fi

build_dir="$(mktemp -d "$apps_dir/.lark-go-build.XXXXXX")"
trap 'rm -rf "$build_dir"' EXIT
(
    cd "$project_dir"
    CGO_ENABLED=1 go build -trimpath -o "$build_dir/lark" ./cmd/lark
)
"$build_dir/lark" --help >/dev/null
hash="$(shasum -a 256 "$build_dir/lark" | cut -d ' ' -f 1)"
marker_value="go:$project_dir:$hash"
marker="$app_dir/Contents/Resources/lark-build-id"

if [[ -e "$app_dir" ]]; then
    if [[ ! -f "$marker" ]]; then
        echo "Refusing to replace an unrecognized app: $app_dir" >&2
        exit 1
    fi
    old_marker="$(cat "$marker")"
    if [[ "$old_marker" != go:"$project_dir":* && "$old_marker" != *":$project_dir/.venv/bin/python" ]]; then
        echo "Refusing to replace a helper from another project: $app_dir" >&2
        exit 1
    fi
fi

if [[ ! -f "$marker" || "$(cat "$marker")" != "$marker_value" ]] || ! codesign --verify --deep --strict "$app_dir" >/dev/null 2>&1; then
    new_app="$build_dir/Lark Helper.app"
    mkdir -p "$new_app/Contents/MacOS" "$new_app/Contents/Resources"
    cp "$build_dir/lark" "$new_app/Contents/MacOS/LarkHelper"
    cat > "$new_app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.akurin.lark.helper</string>
<key>CFBundleName</key><string>Lark Helper</string>
<key>CFBundleDisplayName</key><string>Lark Helper</string>
<key>CFBundleExecutable</key><string>LarkHelper</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.2.0</string>
<key>CFBundleVersion</key><string>2</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>LSUIElement</key><true/>
</dict></plist>
PLIST
    printf '%s\n' "$marker_value" > "$new_app/Contents/Resources/lark-build-id"
    codesign --force --sign - --timestamp=none "$new_app"
    codesign --verify --deep --strict "$new_app"
    if [[ -e "$app_dir" ]]; then mv "$app_dir" "$build_dir/previous.app"; fi
    if ! mv "$new_app" "$app_dir"; then
        if [[ -e "$build_dir/previous.app" ]]; then mv "$build_dir/previous.app" "$app_dir"; fi
        echo "Could not install Lark Helper." >&2
        exit 1
    fi
fi

mv "$build_dir/lark" "$binary"
ln -sfn "$binary" "$link_path"
echo "Installed Go CLI: $link_path"
echo "Installed Go helper: $app_dir"
case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "Add $bin_dir to PATH yourself; no shell configuration was changed." >&2 ;;
esac
echo "Run lark doctor. If prompted, grant Input Monitoring to Lark Helper only."

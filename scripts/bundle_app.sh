#!/usr/bin/env bash
set -euo pipefail

BIN_SRC="${1:-}"
APP_DEST="${2:-}"

if [[ -z "$BIN_SRC" || -z "$APP_DEST" ]]; then
    echo "Usage: $0 <path_to_binary> <path_to_app_bundle>"
    exit 1
fi

if [[ ! -f "$BIN_SRC" ]]; then
    echo "Error: Binary not found at $BIN_SRC"
    exit 1
fi

CONTENTS_DIR="${APP_DEST}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

echo "==> Creating bundle structure: ${APP_DEST}"
mkdir -p "${MACOS_DIR}" "${RESOURCES_DIR}"

echo "==> Copying executable..."
cp -f "${BIN_SRC}" "${MACOS_DIR}/TelifonUI"
chmod +x "${MACOS_DIR}/TelifonUI"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "${SCRIPT_DIR}")"
PLIST_SRC="${ROOT_DIR}/assets/Info.plist"
ICON_SRC="${ROOT_DIR}/assets/AppIcon.icns"

if [[ -f "${PLIST_SRC}" ]]; then
    echo "==> Copying Info.plist..."
    cp -f "${PLIST_SRC}" "${CONTENTS_DIR}/Info.plist"
else
    echo "==> Generating fallback Info.plist..."
    cat <<EOF > "${CONTENTS_DIR}/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleExecutable</key>
    <string>TelifonUI</string>
    <key>CFBundleIdentifier</key>
    <string>com.telifon.proxy</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>Telifon</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>CFBundleVersion</key>
    <string>1</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF
fi

if [[ -f "${ICON_SRC}" ]]; then
    echo "==> Copying AppIcon.icns..."
    cp -f "${ICON_SRC}" "${RESOURCES_DIR}/AppIcon.icns"
fi

echo "==> Bundle successfully created at ${APP_DEST}"

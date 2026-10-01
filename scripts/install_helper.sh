#!/usr/bin/env bash
set -euo pipefail

CORE_BIN="${1:-}"
PLIST_PATH="/Library/LaunchDaemons/com.telifon.cored.plist"
INSTALL_DIR="/Library/PrivilegedHelperTools"
TARGET_BIN="${INSTALL_DIR}/telifon-cored"

if [[ $EUID -ne 0 ]]; then
   echo "Error: This script must be run as root (use sudo)."
   exit 1
fi

if [[ -z "$CORE_BIN" || ! -f "$CORE_BIN" ]]; then
    echo "Usage: sudo $0 <path_to_telifon_cored>"
    exit 1
fi

echo "==> Creating install directory: ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"
mkdir -p "/var/run/telifon"
chmod 0775 "/var/run/telifon"
chown root:admin "/var/run/telifon"

echo "==> Copying daemon binary to ${TARGET_BIN}..."
cp -f "${CORE_BIN}" "${TARGET_BIN}"
chmod 0755 "${TARGET_BIN}"
chown root:wheel "${TARGET_BIN}"

echo "==> Generating LaunchDaemon plist at ${PLIST_PATH}..."
cat <<EOF > "${PLIST_PATH}"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.telifon.cored</string>
    <key>ProgramArguments</key>
    <array>
        <string>${TARGET_BIN}</string>
        <string>--socket</string>
        <string>/var/run/telifon/telifon.sock</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/var/log/telifon-cored.err.log</string>
    <key>StandardOutPath</key>
    <string>/var/log/telifon-cored.out.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>GODEBUG</key>
        <string>madvdontneed=1</string>
    </dict>
</dict>
</plist>
EOF

chmod 0644 "${PLIST_PATH}"
chown root:wheel "${PLIST_PATH}"

echo "==> Bootstrapping daemon with launchctl..."
launchctl bootout system "${PLIST_PATH}" 2>/dev/null || true
launchctl bootstrap system "${PLIST_PATH}"

echo "==> telifon-cored successfully installed and running!"

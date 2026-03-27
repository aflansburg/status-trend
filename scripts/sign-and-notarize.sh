#!/bin/bash
set -euo pipefail

BINARY="$1"
APPLE_ID="abeflansburg@gmail.com"
TEAM_ID="3V2V2L73Z5"
IDENTITY="Developer ID Application: Abram Flansburg (3V2V2L73Z5)"

# Only sign macOS binaries
if [[ "$BINARY" != *"darwin"* ]]; then
  echo "Skipping non-macOS binary: $BINARY"
  exit 0
fi

echo "Signing: $BINARY"
codesign --force --options runtime --sign "$IDENTITY" "$BINARY"
codesign --verify --verbose "$BINARY"

echo "Notarizing: $BINARY"
ZIP_PATH="${BINARY}.zip"
ditto -c -k --keepParent "$BINARY" "$ZIP_PATH"

xcrun notarytool submit "$ZIP_PATH" \
  --apple-id "$APPLE_ID" \
  --team-id "$TEAM_ID" \
  --password "$APPLE_NOTARY_PASSWORD" \
  --wait

rm -f "$ZIP_PATH"
echo "Signed and notarized: $BINARY"

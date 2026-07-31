#!/usr/bin/env bash
# Downloads the Clash of Clans game data used to build the ID catalog, then
# regenerates data/catalog.json.
#
# Run this after a game update so new buildings, levels, troops and hero
# equipment are named and measured correctly.
#
#   ./scripts/fetch-gamedata.sh
#
# REPO can point at any mirror of ClashKingAssets' layout: a static_data.json
# with per-entity level data, and a manifest.json listing icon asset paths.
set -euo pipefail

REPO="${REPO:-https://raw.githubusercontent.com/ClashKingInc/ClashKingAssets/main/assets}"
DEST="${DEST:-gamedata}"

mkdir -p "$DEST"

printf 'static_data.json   '
curl -fsS "$REPO/static_data.json" -o "$DEST/static_data.json" && echo "ok"

printf 'manifest.json      '
curl -fsS "$REPO/manifest.json" -o "$DEST/manifest.json" && echo "ok (icons)" || echo "missing (icons will be skipped)"

echo
go run ./cmd/catalogen -data "$DEST" -out data/catalog.json -source "$REPO"
echo
echo "Rebuild the server to embed the new catalog:  go build ."

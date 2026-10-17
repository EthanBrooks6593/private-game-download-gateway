#!/bin/sh
set -eu

: "${SESSION_ID:?set SESSION_ID to an active game session}"

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data "{\"session_id\":\"${SESSION_ID}\",\"asset_type\":\"player_asset\",\"asset_id\":\"avatar-42.png\"}" \
  http://127.0.0.1:8080/downloads

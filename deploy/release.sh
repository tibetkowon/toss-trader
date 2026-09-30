#!/bin/bash
# Builds the linux/amd64 trader and publishes it to the private release bucket.
# The VM pulls it at its next service start (deploy/fetch-release.sh); a running
# process is never touched. The checksum is uploaded last so it acts as the commit marker.
set -euo pipefail

PROJECT=micro-trading-495614
BUCKET=toss-trader-release-495614

cd "$(dirname "$0")/.."
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

GOOS=linux GOARCH=amd64 go build -o "$out/trader-linux-amd64" ./cmd/trader
(cd "$out" && shasum -a 256 trader-linux-amd64 > trader-linux-amd64.sha256)

gcloud storage cp --project "$PROJECT" "$out/trader-linux-amd64" "gs://$BUCKET/trader/trader-linux-amd64"
gcloud storage cp --project "$PROJECT" "$out/trader-linux-amd64.sha256" "gs://$BUCKET/trader/trader-linux-amd64.sha256"
echo "released $(git rev-parse --short HEAD): $(cut -d' ' -f1 "$out/trader-linux-amd64.sha256")"

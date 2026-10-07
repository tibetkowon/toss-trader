#!/bin/bash
# Builds the linux/amd64 trader + dailyreport and publishes them to the private
# release bucket. The VM pulls them at its next service start
# (deploy/fetch-release.sh); a running process is never touched. Each binary's
# checksum is uploaded right after it, last, so it acts as that binary's commit marker.
set -euo pipefail

PROJECT=micro-trading-495614
BUCKET=toss-trader-release-495614

cd "$(dirname "$0")/.."
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

rev=$(git rev-parse --short HEAD)
for name in trader dailyreport; do
  GOOS=linux GOARCH=amd64 go build -o "$out/$name-linux-amd64" "./cmd/$name"
  (cd "$out" && shasum -a 256 "$name-linux-amd64" > "$name-linux-amd64.sha256")
  gcloud storage cp --project "$PROJECT" "$out/$name-linux-amd64" "gs://$BUCKET/$name/$name-linux-amd64"
  gcloud storage cp --project "$PROJECT" "$out/$name-linux-amd64.sha256" "gs://$BUCKET/$name/$name-linux-amd64.sha256"
  echo "released $name $rev: $(cut -d' ' -f1 "$out/$name-linux-amd64.sha256")"
done

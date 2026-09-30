#!/bin/bash
# Runs as root from the unit's ExecStartPre before every start of toss-trader.
# Pulls the latest trader binary from the private release bucket (deploy/release.sh
# uploads it). It must never block or fail the boot: any problem leaves the current
# binary in place.
set -u

DEST=/opt/toss-trader/trader
OBJ=trader/trader-linux-amd64
BUCKET="${RELEASE_BUCKET:-}"

skip() { echo "fetch-release: $* — keeping current binary"; exit 0; }

[ -n "$BUCKET" ] || skip "RELEASE_BUCKET unset"

token=$(curl -sf --max-time 10 -H 'Metadata-Flavor: Google' \
  'http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token' |
  sed -n 's/.*"access_token" *: *"\([^"]*\)".*/\1/p')
[ -n "$token" ] || skip "could not get an access token"

get() { # object, output file
  curl -sf --max-time 60 -H "Authorization: Bearer $token" \
    "https://storage.googleapis.com/storage/v1/b/$BUCKET/o/${1//\//%2F}?alt=media" -o "$2"
}

tmp=$(mktemp -d /opt/toss-trader/.fetch.XXXXXX) || skip "no temp dir"
trap 'rm -rf "$tmp"' EXIT

get "$OBJ.sha256" "$tmp/sha" || skip "checksum not available"
want=$(awk '{print $1}' "$tmp/sha")
[ ${#want} -eq 64 ] || skip "malformed checksum"

have=$(sha256sum "$DEST" 2>/dev/null | awk '{print $1}')
[ "$want" != "$have" ] || { echo "fetch-release: already up to date ($want)"; exit 0; }

get "$OBJ" "$tmp/bin" || skip "binary download failed"
got=$(sha256sum "$tmp/bin" | awk '{print $1}')
[ "$got" = "$want" ] || skip "checksum mismatch (got $got, want $want)"
[ "$(head -c4 "$tmp/bin" | od -An -c | tr -d ' ')" = '177ELF' ] || skip "not an ELF binary"

[ ! -f "$DEST" ] || cp -p "$DEST" "$DEST.prev"
install -m 0755 "$tmp/bin" "$DEST.tmp" && mv "$DEST.tmp" "$DEST" || skip "install failed"
echo "fetch-release: installed $want (previous kept as trader.prev)"

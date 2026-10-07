#!/bin/bash
# Runs as root from the unit's ExecStartPre before every start of toss-trader.
# Pulls the latest trader + dailyreport binaries from the private release bucket
# (deploy/release.sh uploads them). It must never block or fail the boot: any
# problem leaves the current binaries in place. trader is fetched first since it's
# the safety-critical one; dailyreport is best-effort and never affects trading.
set -u

DIR=/opt/toss-trader
BUCKET="${RELEASE_BUCKET:-}"

skip() { echo "fetch-release: $* — keeping current binaries"; exit 0; }

[ -n "$BUCKET" ] || skip "RELEASE_BUCKET unset"

token=$(curl -sf --max-time 10 -H 'Metadata-Flavor: Google' \
  'http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token' |
  sed -n 's/.*"access_token" *: *"\([^"]*\)".*/\1/p')
[ -n "$token" ] || skip "could not get an access token"

get() { # object, output file
  curl -sf --max-time 60 -H "Authorization: Bearer $token" \
    "https://storage.googleapis.com/storage/v1/b/$BUCKET/o/${1//\//%2F}?alt=media" -o "$2"
}

# fetch_binary name dest obj — best-effort; any problem just logs and leaves dest alone.
fetch_binary() {
  local name="$1" dest="$2" obj="$3" tmp
  tmp=$(mktemp -d "$DIR/.fetch.XXXXXX") || { echo "fetch-release: $name: no temp dir"; return; }
  (
    trap 'rm -rf "$tmp"' EXIT
    get "$obj.sha256" "$tmp/sha" || { echo "fetch-release: $name: checksum not available"; exit 0; }
    want=$(awk '{print $1}' "$tmp/sha")
    [ ${#want} -eq 64 ] || { echo "fetch-release: $name: malformed checksum"; exit 0; }

    have=$(sha256sum "$dest" 2>/dev/null | awk '{print $1}')
    [ "$want" != "$have" ] || { echo "fetch-release: $name: already up to date ($want)"; exit 0; }

    get "$obj" "$tmp/bin" || { echo "fetch-release: $name: binary download failed"; exit 0; }
    got=$(sha256sum "$tmp/bin" | awk '{print $1}')
    [ "$got" = "$want" ] || { echo "fetch-release: $name: checksum mismatch (got $got, want $want)"; exit 0; }
    [ "$(head -c4 "$tmp/bin" | od -An -c | tr -d ' ')" = '177ELF' ] || { echo "fetch-release: $name: not an ELF binary"; exit 0; }

    [ ! -f "$dest" ] || cp -p "$dest" "$dest.prev"
    install -m 0755 "$tmp/bin" "$dest.tmp" && mv "$dest.tmp" "$dest" || { echo "fetch-release: $name: install failed"; exit 0; }
    echo "fetch-release: $name: installed $want (previous kept as $(basename "$dest").prev)"
  )
}

fetch_binary trader "$DIR/trader" trader/trader-linux-amd64
fetch_binary dailyreport "$DIR/dailyreport" dailyreport/dailyreport-linux-amd64

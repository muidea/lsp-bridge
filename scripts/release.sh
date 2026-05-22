#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-}"
REPO="${REPO:-muidea/lsp-bridge}"
DIST_DIR="${DIST_DIR:-dist}"

if [ -z "$VERSION" ]; then
  printf 'usage: %s <version>\n' "$0" >&2
  printf 'example: %s v0.1.0\n' "$0" >&2
  exit 2
fi

case "$VERSION" in
  v*) ;;
  *) VERSION="v${VERSION}" ;;
esac

log() {
  printf '[lsp-bridge release] %s\n' "$*"
}

build_one() {
  local goos="$1"
  local goarch="$2"
  local name="lsp-bridge_${VERSION}_${goos}_${goarch}"
  local work="${DIST_DIR}/${name}"
  local binary="${work}/lsp-bridge"

  if [ "$goos" = "windows" ]; then
    binary="${binary}.exe"
  fi

  mkdir -p "$work"
  log "building ${goos}/${goarch}"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$binary" ./cmd/lsp-bridge
  cp README.md LICENSE "$work/"
  tar -C "$work" -czf "${DIST_DIR}/${name}.tar.gz" .
  rm -rf "$work"
}

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

log "running tests"
go test ./...

build_one linux amd64
build_one linux arm64
build_one darwin amd64
build_one darwin arm64

(
  cd "$DIST_DIR"
  sha256sum *.tar.gz > checksums.txt
)

log "release artifacts:"
ls -1 "$DIST_DIR"

if [ "${PUBLISH:-0}" = "1" ]; then
  command -v gh >/dev/null 2>&1 || {
    printf 'gh is required when PUBLISH=1\n' >&2
    exit 1
  }
  log "publishing ${VERSION} to ${REPO}"
  gh release create "$VERSION" "${DIST_DIR}"/*.tar.gz "${DIST_DIR}/checksums.txt" \
    --repo "$REPO" \
    --title "$VERSION" \
    --notes "Release ${VERSION}"
fi

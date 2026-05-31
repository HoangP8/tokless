#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo 0.0.0)}"
VERSION="${VERSION#v}"
PKG="github.com/HoangP8/tokless/internal/util"
OUT="dist/release"

rm -rf "$OUT"
mkdir -p "$OUT"

TARGETS="
tokless-linux-x64 linux amd64
tokless-linux-arm64 linux arm64
tokless-darwin-x64 darwin amd64
tokless-darwin-arm64 darwin arm64
tokless-windows-x64.exe windows amd64
"

echo "$TARGETS" | while read -r asset goos goarch; do
  [ -z "$asset" ] && continue
  printf 'compiling %s (%s/%s) … ' "$asset" "$goos" "$goarch"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X ${PKG}.Version=${VERSION}" \
    -o "${OUT}/${asset}" ./cmd/tokless
  echo "ok"
done

echo
echo "Built binaries for v${VERSION} in ${OUT}/:"
ls -lh "$OUT"

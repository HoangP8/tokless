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

SYSO=cmd/tokless/rsrc_windows_amd64.syso
trap 'rm -f "$SYSO"' EXIT

echo "$TARGETS" | while read -r asset goos goarch; do
  [ -z "$asset" ] && continue
  printf 'compiling %s (%s/%s) … ' "$asset" "$goos" "$goarch"
  ldflags="-s -w -X ${PKG}.Version=${VERSION}"
  if [ "$goos" = windows ]; then
    ldflags="-X ${PKG}.Version=${VERSION}"
    go run github.com/tc-hib/go-winres@v0.3.3 make --arch "$goarch" \
      --in cmd/tokless/winres/winres.json --out cmd/tokless/rsrc \
      --product-version "$VERSION" --file-version "$VERSION"
  fi
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$ldflags" \
    -o "${OUT}/${asset}" ./cmd/tokless
  rm -f "$SYSO"
  echo "ok"
done

(
  cd "$OUT"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum tokless-* | LC_ALL=C sort -k2 > SHA256SUMS
  else
    shasum -a 256 tokless-* | LC_ALL=C sort -k2 > SHA256SUMS
  fi
)

echo
echo "Built binaries for v${VERSION} in ${OUT}/:"
ls -lh "$OUT"

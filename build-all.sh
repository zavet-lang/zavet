#!/usr/bin/env bash
# Скрипт сборки дистрибутивов языка Завет.
set -euo pipefail

VERSION="${1:-1.1.0}"
OUT="dist/release-${VERSION}"
rm -rf "$OUT"
mkdir -p "$OUT"

echo "Сборка дистрибутивов Завет v${VERSION}..."

build_target() {
    local goos="$1" goarch="$2" ext="$3"
    local bin="zavet${ext}"
    local stage="${OUT}/stage_${goos}_${goarch}"
    mkdir -p "$stage"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
        -ldflags "-s -w -X main.Version=${VERSION}" \
        -o "${stage}/${bin}" .
    cp README.md LICENSE "$stage/" 2>/dev/null || true

    if [ "$goos" = "windows" ]; then
        (cd "$stage" && zip -q "../zavet_${VERSION}_${goos}_${goarch}.zip" .)
    else
        tar -czf "${OUT}/zavet_${VERSION}_${goos}_${goarch}.tar.gz" -C "$stage" .
    fi
    rm -rf "$stage"
    echo "  ✓ zavet_${VERSION}_${goos}_${goarch}"
}

build_target linux amd64 ""
build_target linux arm64 ""
build_target darwin amd64 ""
build_target darwin arm64 ""
build_target windows amd64 ".exe"

echo "Готово: ${OUT}"

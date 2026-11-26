#!/usr/bin/env sh
# Сборка .deb-пакета Завета для Debian/Ubuntu (apt).
set -eu

VERSION="${1:-1.1.0}"
ARCH="${2:-amd64}"
PKG_DIR="dist/deb/zavet_${VERSION}_${ARCH}"

rm -rf "$PKG_DIR"
mkdir -p "$PKG_DIR/DEBIAN" "$PKG_DIR/usr/bin"

GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.Version=$VERSION" \
    -o "$PKG_DIR/usr/bin/zavet" .

cat > "$PKG_DIR/DEBIAN/control" <<EOF
Package: zavet
Version: ${VERSION}
Section: devel
Priority: optional
Architecture: ${ARCH}
Maintainer: Zavet Maintainers
Description: Язык программирования Завет на библейском русском
 Интерпретатор, байткод-VM, форматтер, линтер, отладчик, LSP и MCP для языка Завет.
EOF

chmod 755 "$PKG_DIR/usr/bin/zavet"
dpkg-deb --build "$PKG_DIR"
echo "Готово: ${PKG_DIR}.deb"

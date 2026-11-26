#!/usr/bin/env sh
# Установщик языка Завет.
#
# Что делает:
#   1) Определяет операционную систему и архитектуру.
#   2) Качает нужный архив с GitHub Releases (формат .goreleaser.yml).
#   3) Проверяет SHA256-сумму по checksums.txt.
#   4) Распаковывает в $HOME/.zavet и добавляет $HOME/.zavet/bin в PATH.
#
# Использование:
#   curl -fsSL https://raw.githubusercontent.com/zavet-lang/zavet/main/install.sh | sh
#
# Переменные окружения (необязательные):
#   ZAVET_VERSION          — версия (по умолчанию 1.1.0)
#   ZAVET_INSTALL_DIR      — куда устанавливать (по умолчанию $HOME/.zavet)
#   ZAVET_REPO             — GitHub-репозиторий (по умолчанию zavet-lang/zavet)
#   ZAVET_NO_MODIFY_PATH=1 — не править .bashrc / .zshrc / .profile

set -eu

VERSION="${ZAVET_VERSION:-1.1.0}"
INSTALL_DIR="${ZAVET_INSTALL_DIR:-$HOME/.zavet}"
BIN_DIR="$INSTALL_DIR/bin"
REPO="${ZAVET_REPO:-zavet-lang/zavet}"
DOWNLOAD_BASE="https://github.com/$REPO/releases/download/v$VERSION"

# === Утилиты ===
say() { printf '\033[1;33m▸\033[0m %s\n' "$1"; }
ok()  { printf '\033[1;32m✓\033[0m %s\n' "$1"; }
err() { printf '\033[1;31m✗\033[0m %s\n' "$1" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || err "Нужна утилита '$1', но её нет в PATH"
}

need uname
need tar

if command -v curl >/dev/null 2>&1; then
    DL_CMD="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
    DL_CMD="wget -q -O -"
else
    err "Нужен curl или wget для скачивания"
fi

# === Определяем платформу ===
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
    linux)  PLATFORM_OS="linux" ;;
    darwin) PLATFORM_OS="darwin" ;;
    *)      err "ОС '$OS' не поддерживается. Только Linux и macOS. Для Windows используйте zip с GitHub Releases." ;;
esac

case "$ARCH" in
    x86_64|amd64)   PLATFORM_ARCH="amd64" ;;
    arm64|aarch64)  PLATFORM_ARCH="arm64" ;;
    *)              err "Архитектура '$ARCH' не поддерживается. Только amd64 и arm64." ;;
esac

ARCHIVE="zavet_${VERSION}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
URL="$DOWNLOAD_BASE/$ARCHIVE"

# === Шапка ===
printf '\n'
printf '\033[1;33m'
printf 'ЗАВЕТ — установщик\n'
printf '\033[0m'
printf '\n'
say "Версия:    $VERSION"
say "Платформа: $PLATFORM_OS-$PLATFORM_ARCH"
say "Папка:     $INSTALL_DIR"
printf '\n'

# === Скачиваем ===
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say "Скачиваю $ARCHIVE..."
if ! $DL_CMD "$URL" > "$TMP/$ARCHIVE"; then
    err "Не удалось скачать. Проверьте интернет и URL: $URL"
fi
ok "Скачано $(wc -c < "$TMP/$ARCHIVE" | awk '{print int($1/1024/1024)}') МБ"

# === Проверяем SHA256 ===
say "Проверяю SHA256..."
EXPECTED_SUMS_URL="$DOWNLOAD_BASE/checksums.txt"
if $DL_CMD "$EXPECTED_SUMS_URL" > "$TMP/checksums.txt" 2>/dev/null; then
    EXPECTED=$(grep "  $ARCHIVE\$" "$TMP/checksums.txt" | awk '{print $1}')
    if [ -z "$EXPECTED" ]; then
        say "SHA256 для $ARCHIVE не найдена в checksums.txt, пропускаю проверку"
    else
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL=$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')
        elif command -v shasum >/dev/null 2>&1; then
            ACTUAL=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')
        else
            ACTUAL=""
            say "Нет sha256sum/shasum, пропускаю проверку"
        fi
        if [ -n "$ACTUAL" ]; then
            if [ "$ACTUAL" != "$EXPECTED" ]; then
                err "SHA256 не совпала! Ожидалось $EXPECTED, получено $ACTUAL"
            fi
            ok "SHA256 проверена"
        fi
    fi
else
    say "checksums.txt недоступен, пропускаю проверку"
fi

# === Установка ===
mkdir -p "$BIN_DIR"
say "Распаковываю в $INSTALL_DIR..."
tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
mv "$TMP/zavet" "$BIN_DIR/zavet"
chmod +x "$BIN_DIR/zavet"

[ -f "$TMP/README.md" ] && cp "$TMP/README.md" "$INSTALL_DIR/" 2>/dev/null || true

ok "Установлено в $BIN_DIR/zavet"

# === Добавляем в PATH ===
add_path_to() {
    profile="$1"
    [ -f "$profile" ] || return 0
    if grep -qF '.zavet/bin' "$profile" 2>/dev/null; then
        return 0
    fi
    {
        printf '\n# Завет\n'
        printf 'export PATH="%s:$PATH"\n' "$BIN_DIR"
    } >> "$profile"
    ok "Добавлено в PATH через $profile"
    PATH_MODIFIED=1
}

PATH_MODIFIED=0
if [ "${ZAVET_NO_MODIFY_PATH:-0}" != "1" ]; then
    case "${SHELL:-/bin/sh}" in
        */zsh)  add_path_to "$HOME/.zshrc"   ;;
        */bash) add_path_to "$HOME/.bashrc"  ;;
        */fish) say "Fish: добавь в config.fish — set -x PATH $BIN_DIR \$PATH" ;;
        *)      add_path_to "$HOME/.profile" ;;
    esac
fi

# === Финал ===
printf '\n'
ok "Установка завершена"
printf '\n'
"$BIN_DIR/zavet" помощь 2>/dev/null | head -3 || "$BIN_DIR/zavet" --version 2>/dev/null || true
printf '\n'
say "Что дальше:"
if [ "$PATH_MODIFIED" = "1" ]; then
    printf '   Перезапустите терминал или выполните:\n'
    printf '     source %s\n' "$(case "${SHELL:-/bin/sh}" in */zsh) echo "$HOME/.zshrc";; */bash) echo "$HOME/.bashrc";; *) echo "$HOME/.profile";; esac)"
fi
printf '   Создать новый проект:\n'
printf '     zavet инит мой_проект\n'
printf '   Запустить файл:\n'
printf '     zavet главный.молитва\n'
printf '\n'

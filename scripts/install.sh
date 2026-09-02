#!/bin/sh
# mcp-server-magictools bootstrap installer.
#
#   curl -fsSL https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.sh | sh
#
# Downloads and verifies the release binary, then places it in ~/.local/bin.
# It never runs configure, init, or service installation.
set -eu

REPO_URL="https://github.com/maccavelli/mcp-server-magictools/releases"
PRODUCT="mcp-server-magictools"
TMP_DIR=""

log()  { printf '%s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$2" >&2; exit "$1"; }
vlog() {
    if [ "${VERBOSE:-0}" = 1 ]; then
        printf '  %s\n' "$*" >&2
    fi
}

# shellcheck disable=SC2329,SC2317
cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf "$TMP_DIR"
    fi
}

have() { command -v "$1" >/dev/null 2>&1; }

fetch() {
    case "$1" in
        /*) [ -f "$1" ] || return 1; cp -f "$1" "$2" ;;
        *)
            if have curl; then
                curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
            else
                wget -q -O "$2" "$1"
            fi
            ;;
    esac
}

sha256_of() {
    if have sha256sum; then
        sha256sum "$1" | awk '{print $1}'
    elif have shasum; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    fi
}

detect_target() {
    uname_s=$(uname -s)
    uname_m=$(uname -m)

    case "$uname_s" in
        Linux)
            OS=linux
            case "$uname_m" in
                x86_64|amd64) ARCH=amd64 ;;
                aarch64|arm64) die 1 "linux/arm64 is not a published target" ;;
                *) die 1 "unsupported Linux architecture $uname_m; only amd64 is published" ;;
            esac
            ;;
        Darwin)
            OS=darwin
            case "$uname_m" in
                arm64|aarch64) ARCH=arm64 ;;
                x86_64|amd64) die 1 "darwin/amd64 is not a published target" ;;
                *) die 1 "unsupported macOS architecture $uname_m; only arm64 is published" ;;
            esac
            ;;
        *)
            die 1 "this installer supports Linux and macOS only (found $uname_s).
Windows: irm https://github.com/maccavelli/mcp-server-magictools/releases/latest/download/install.ps1 | iex"
            ;;
    esac
}

verify_and_resolve() {
    # Canonical releases (MADR 0005) list the exact downloaded basename;
    # pre-canonical releases listed a version-suffixed name. Both are looked up
    # and a manifest carrying BOTH is ambiguous: preferring either shape would
    # let an appended line authorize a substituted binary, so it fails closed.
    canon=$(grep -E "  ${PRODUCT}-${OS}-${ARCH}$" "$TMP_DIR/SHA256SUMS" | head -n 1) || true
    versioned=$(grep -E "  ${PRODUCT}-${OS}-${ARCH}-[0-9]" "$TMP_DIR/SHA256SUMS" | head -n 1) || true
    if [ -n "$canon" ] && [ -n "$versioned" ]; then
        die 2 "ambiguous SHA256SUMS: both a canonical and a versioned entry exist for ${PRODUCT}-${OS}-${ARCH}
A conforming release lists one shape per manifest. Refusing to choose.
Nothing was installed."
    fi
    line=$canon
    [ -n "$line" ] || line=$versioned
    [ -n "$line" ] || die 2 "no checksum entry for ${PRODUCT}-${OS}-${ARCH} in SHA256SUMS"

    want=$(printf '%s\n' "$line" | awk '{print $1}')
    name=$(printf '%s\n' "$line" | awk '{print $NF}')
    got=$(sha256_of "$TMP_DIR/$PRODUCT")

    if [ "$want" != "$got" ]; then
        die 2 "checksum mismatch for $PRODUCT
  expected $want
  got      $got
Nothing was installed."
    fi
    RESOLVED_VER=${name#"${PRODUCT}-${OS}-${ARCH}-"}
    RESOLVED_VER=${RESOLVED_VER%.exe}
    vlog "$PRODUCT verified, version $RESOLVED_VER"
}

download_all() {
    if [ -n "$PIN_VERSION" ]; then
        URL_DIR="$BASE_URL/download/v$PIN_VERSION"
    else
        URL_DIR="$BASE_URL/latest/download"
    fi
    vlog "source $URL_DIR"

    fetch "$URL_DIR/SHA256SUMS" "$TMP_DIR/SHA256SUMS" ||
        die 2 "could not download SHA256SUMS from $URL_DIR"
    fetch "$URL_DIR/${PRODUCT}-${OS}-${ARCH}" "$TMP_DIR/$PRODUCT" ||
        die 2 "could not download ${PRODUCT}-${OS}-${ARCH} from $URL_DIR"
    verify_and_resolve
}

install_binary() {
    chmod 0755 "$TMP_DIR/$PRODUCT"
    target="$INSTALL_DIR/$PRODUCT"
    if [ -e "$target" ]; then
        rm -f "${target}.prev"
        mv -f "$target" "${target}.prev" || die 2 "cannot move aside $target"
    fi
    mv -f "$TMP_DIR/$PRODUCT" "$target" || die 2 "cannot install $PRODUCT to $INSTALL_DIR"
    log "installed $target"
}

check_path() {
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            log ""
            log "note: $INSTALL_DIR is not on your PATH. Add it with:"
            log "    export PATH=\"\$PATH:$INSTALL_DIR\""
            ;;
    esac
}

do_uninstall() {
    target="$INSTALL_DIR/$PRODUCT"
    if [ -e "$target" ]; then
        rm -f "$target" && log "removed $target"
    else
        log "nothing to remove at $target"
    fi
    rm -f "${target}.prev" 2>/dev/null || true
}

usage() {
    cat >&2 <<'EOF'
mcp-server-magictools Linux / macOS installer

  install.sh [--version X.Y.Z] [--dir PATH] [--dry-run]
             [--verbose] [--uninstall] [--help]

Piped invocation can use environment variables:

  MCP_MAGICTOOLS_VERSION      release version to install
  MCP_MAGICTOOLS_INSTALL_DIR  destination directory (default ~/.local/bin)

The installer only places the verified binary. It does not configure or start
MagicTools and does not install a background service.
EOF
}

main() {
    INSTALL_DIR="${MCP_MAGICTOOLS_INSTALL_DIR:-$HOME/.local/bin}"
    PIN_VERSION="${MCP_MAGICTOOLS_VERSION:-}"
    BASE_URL="${MC_TEST_BASE_URL:-$REPO_URL}"
    DRY_RUN=0
    VERBOSE=0
    UNINSTALL=0
    RESOLVED_VER=""

    while [ "$#" -gt 0 ]; do
        case "$1" in
            --version) PIN_VERSION="${2:?--version needs a value}"; shift 2 ;;
            --dir)     INSTALL_DIR="${2:?--dir needs a value}"; shift 2 ;;
            --dry-run) DRY_RUN=1; shift ;;
            --verbose|-v) VERBOSE=1; shift ;;
            --uninstall) UNINSTALL=1; shift ;;
            --help|-h) usage; exit 0 ;;
            *) usage; die 1 "unknown option: $1" ;;
        esac
    done

    PIN_VERSION=${PIN_VERSION#v}
    if [ -n "$PIN_VERSION" ] && ! printf '%s\n' "$PIN_VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
        die 1 "version must be X.Y.Z (got $PIN_VERSION)"
    fi

    detect_target

    if [ "$UNINSTALL" = 1 ]; then
        do_uninstall
        exit 0
    fi

    if [ "$DRY_RUN" = 1 ]; then
        log "dry run — nothing will be written"
        log "  os:          $OS"
        log "  arch:        $ARCH"
        log "  install dir: $INSTALL_DIR"
        log "  action:      place binary only"
        if [ -n "$PIN_VERSION" ]; then
            log "  source:      $BASE_URL/download/v$PIN_VERSION"
        else
            log "  source:      $BASE_URL/latest/download"
        fi
        exit 0
    fi

    missing=""
    case "$BASE_URL" in
        /*) ;;
        *) have curl || have wget || missing="$missing curl-or-wget" ;;
    esac
    have sha256sum || have shasum || have openssl || missing="$missing sha256-tool"
    have awk || missing="$missing awk"
    have grep || missing="$missing grep"
    [ -z "$missing" ] || die 1 "missing required tools:$missing"

    mkdir -p "$INSTALL_DIR" || die 1 "cannot create $INSTALL_DIR"
    INSTALL_DIR=$(CDPATH='' cd -- "$INSTALL_DIR" && pwd) || die 1 "cannot resolve $INSTALL_DIR"
    TMP_DIR=$(mktemp -d "$INSTALL_DIR/.magictools-install.XXXXXX") || die 1 "cannot create temporary directory"
    trap cleanup EXIT INT TERM

    download_all
    install_binary
    check_path

    log ""
    log "$PRODUCT ${RESOLVED_VER:-unknown} installed to $INSTALL_DIR"
}

main "$@"

#!/bin/sh
# Offline tests for scripts/install.sh.
set -eu

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
INSTALLER="$HERE/install.sh"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; [ "$#" -gt 1 ] && printf '       %s\n' "$2"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want [$3] got [$2]"; fi; }
contains() { case "$2" in *"$3"*) ok "$1" ;; *) bad "$1" "missing [$3] in: $2" ;; esac; }

sha_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

VER=9.9.9
PRODUCT=mcp-server-magictools

mk_release() {
    root=$1
    arch=$2
    os=${3:-linux}
    corrupt=${4:-}
    rel="$root/latest/download"
    mkdir -p "$rel"
    # shellcheck disable=SC2016 # Generated fixture expands these at runtime.
    printf '#!/bin/sh\ntouch "$(dirname "$0")/.executed"\necho "%s version %s"\n' \
        "$PRODUCT" "$VER" > "$rel/$PRODUCT-$os-$arch"
    chmod 0755 "$rel/$PRODUCT-$os-$arch"
    printf '%s  %s-%s-%s-%s\n' "$(sha_of "$rel/$PRODUCT-$os-$arch")" \
        "$PRODUCT" "$os" "$arch" "$VER" > "$rel/SHA256SUMS"
    if [ "$corrupt" = corrupt ]; then
        printf '#!/bin/sh\necho tampered\n' > "$rel/$PRODUCT-$os-$arch"
        chmod 0755 "$rel/$PRODUCT-$os-$arch"
    fi
}

mk_stubs() {
    dir=$1
    machine=$2
    system=${3:-Linux}
    mkdir -p "$dir"
    for tool in sh mktemp mkdir mv rm chmod cat grep awk cp head dirname touch; do
        path=$(command -v "$tool" 2>/dev/null) || continue
        [ -e "$dir/$tool" ] || ln -sf "$path" "$dir/$tool"
    done
    for tool in sha256sum shasum openssl; do
        path=$(command -v "$tool" 2>/dev/null) || continue
        [ -e "$dir/$tool" ] || ln -sf "$path" "$dir/$tool"
    done
    # shellcheck disable=SC2016 # Generated uname stub expands $1 at runtime.
    printf '#!/bin/sh\ncase "$1" in -m) echo %s ;; -s) echo %s ;; *) echo %s ;; esac\n' \
        "$machine" "$system" "$system" > "$dir/uname"
    chmod 0755 "$dir/uname"
}

run_installer() {
    run_path=$1
    base_url=$2
    install_dir=$3
    shift 3
    (
        set +e
        PATH="$run_path"; export PATH
        if [ "$base_url" != - ]; then
            MC_TEST_BASE_URL="$base_url"; export MC_TEST_BASE_URL
        fi
        MCP_MAGICTOOLS_INSTALL_DIR="$install_dir"; export MCP_MAGICTOOLS_INSTALL_DIR
        "$INSTALLER" "$@" >"$WORK/out" 2>"$WORK/err"
        echo "$?" > "$WORK/rc"
    )
    RC=$(cat "$WORK/rc")
    OUT=$(cat "$WORK/out" "$WORK/err" 2>/dev/null || true)
}

printf '\n1. target mapping\n'

LINUX="$WORK/stub-linux"
mk_stubs "$LINUX" x86_64 Linux
run_installer "$LINUX" - "$WORK/bin-linux-dry" --dry-run
check "linux/amd64 dry run succeeds" "$RC" 0
contains "  reports linux" "$OUT" "os:          linux"
contains "  reports amd64" "$OUT" "arch:        amd64"
contains "  reports binary-only action" "$OUT" "action:      place binary only"

LINUX_ARM="$WORK/stub-linux-arm"
mk_stubs "$LINUX_ARM" arm64 Linux
run_installer "$LINUX_ARM" - "$WORK/bin-linux-arm" --dry-run
check "linux/arm64 is rejected" "$RC" 1
contains "  rejection names unpublished target" "$OUT" "linux/arm64 is not a published target"

DARWIN="$WORK/stub-darwin"
mk_stubs "$DARWIN" arm64 Darwin
run_installer "$DARWIN" - "$WORK/bin-darwin-dry" --dry-run
check "darwin/arm64 dry run succeeds" "$RC" 0
contains "  reports darwin" "$OUT" "os:          darwin"
contains "  reports arm64" "$OUT" "arch:        arm64"

DARWIN_AMD="$WORK/stub-darwin-amd"
mk_stubs "$DARWIN_AMD" x86_64 Darwin
run_installer "$DARWIN_AMD" - "$WORK/bin-darwin-amd" --dry-run
check "darwin/amd64 is rejected" "$RC" 1
contains "  rejection names unpublished target" "$OUT" "darwin/amd64 is not a published target"

FREEBSD="$WORK/stub-freebsd"
mk_stubs "$FREEBSD" amd64 FreeBSD
run_installer "$FREEBSD" - "$WORK/bin-freebsd" --dry-run
check "unsupported OS is rejected" "$RC" 1
contains "  message points Windows users to PowerShell" "$OUT" "install.ps1 | iex"

printf '\n2. download, verification, and installation\n'

R="$WORK/release-missing"
mk_release "$R" amd64
: > "$R/latest/download/SHA256SUMS"
D="$WORK/bin-missing"
run_installer "$LINUX" "$R" "$D"
check "missing checksum entry exits 2" "$RC" 2
check "  missing entry installs nothing" "$( [ -e "$D/$PRODUCT" ] && echo yes || echo no )" no

R="$WORK/release-corrupt"
mk_release "$R" amd64 linux corrupt
D="$WORK/bin-corrupt"
mkdir -p "$D"
printf 'PREEXISTING\n' > "$D/$PRODUCT"
run_installer "$LINUX" "$R" "$D"
check "checksum mismatch exits 2" "$RC" 2
check "  existing binary is untouched" "$(cat "$D/$PRODUCT")" PREEXISTING
check "  temporary directory is removed" "$(find "$D" -maxdepth 1 -name '.magictools-install.*' | wc -l | tr -d ' ')" 0

R="$WORK/release-ok"
mk_release "$R" amd64
D="$WORK/bin-ok"
run_installer "$LINUX" "$R" "$D"
check "valid release installs" "$RC" 0
check "  binary is executable" "$( [ -x "$D/$PRODUCT" ] && echo yes || echo no )" yes
check "  installer does not execute binary" "$( [ -e "$D/.executed" ] && echo yes || echo no )" no
check "  installer does not configure" "$(find "$D" -maxdepth 1 -name '*configure*' | wc -l | tr -d ' ')" 0
check "  installer does not install a service" "$(find "$D" -maxdepth 1 -name '*service*' | wc -l | tr -d ' ')" 0
contains "  resolved version is reported" "$OUT" "$VER"

run_installer "$LINUX" "$R" "$D"
check "reinstall succeeds" "$RC" 0
check "  previous binary is retained" "$( [ -e "$D/${PRODUCT}.prev" ] && echo yes || echo no )" yes
check "  reinstall still does not execute binary" "$( [ -e "$D/.executed" ] && echo yes || echo no )" no
check "  reinstall removes temporary directory" "$(find "$D" -maxdepth 1 -name '.magictools-install.*' | wc -l | tr -d ' ')" 0

PINNED="$WORK/release-pinned"
mk_release "$PINNED" amd64
mkdir -p "$PINNED/download/v$VER"
cp "$PINNED/latest/download/SHA256SUMS" "$PINNED/download/v$VER/SHA256SUMS"
cp "$PINNED/latest/download/$PRODUCT-linux-amd64" "$PINNED/download/v$VER/$PRODUCT-linux-amd64"
run_installer "$LINUX" "$PINNED" "$WORK/bin-pinned" --version "v$VER"
check "pinned version installs" "$RC" 0

run_installer "$LINUX" - "$WORK/bin-bad-version" --version nope --dry-run
check "invalid version is rejected" "$RC" 1

last=$(tail -n 1 "$INSTALLER")
check "script ends with main \"\$@\"" "$last" 'main "$@"'

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]

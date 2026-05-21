#!/bin/sh
# Main entry point — argument parsing and dispatch

main() {
    ACTION="install"
    VERSION=""
    FORCE_ARCH=""

    # Parse arguments first (need QUIET_MODE before tty redirect)
    for arg in "$@"; do
        case "$arg" in
        --remove | --uninstall | -r)
            ACTION="remove"
            ;;
        --update | -u)
            ACTION="update"
            ;;
        --sysinfo | --info | -i)
            ACTION="sysinfo"
            ;;
        --quiet | -q)
            QUIET_MODE=1
            ;;
        --arch=*)
            FORCE_ARCH="${arg#*=}"
            ;;
        --platform=*)
            B4_PLATFORM="${arg#*=}"
            ;;
        --bin-dir=*)
            B4_BIN_DIR="${arg#*=}"
            ;;
        --data-dir=*)
            _dd="${arg#*=}"
            if ! is_abs_path "$_dd"; then
                printf 'ERROR: --data-dir must be an absolute path (got: %s)\n' "${_dd:-empty}" >&2
                exit 1
            fi
            B4_DATA_DIR="$_dd"
            ;;
        --help | -h)
            _show_help
            exit 0
            ;;
        v* | V*)
            VERSION="$arg"
            ;;
        *) ;;
        esac
    done

    # Redirect stdin from tty for piped installs (curl | sh).
    # Skip in quiet mode — no interactive input needed, and /dev/tty
    # may not be available (e.g. web UI update running without a terminal).
    if [ "$QUIET_MODE" -ne 1 ] 2>/dev/null && [ ! -t 0 ] && [ -e /dev/tty ]; then
        exec </dev/tty
    fi

    # Dispatch
    case "$ACTION" in
    install) action_install "$VERSION" "$FORCE_ARCH" ;;
    remove) action_remove ;;
    update) action_update "$VERSION" "$FORCE_ARCH" ;;
    sysinfo) action_sysinfo ;;
    *) ;;
    esac
}

_show_help() {
    echo "B4 Universal Installer"
    echo ""
    echo "Usage: $0 [OPTIONS] [VERSION]"
    echo ""
    echo "Actions:"
    echo "  (default)           Install b4 (interactive wizard)"
    echo "  --update, -u        Update b4 to latest version"
    echo "  --remove, -r        Uninstall b4"
    echo "  --sysinfo, -i       Show system diagnostics"
    echo ""
    echo "Options:"
    echo "  --arch=ARCH         Force architecture (skip detection)"
    echo "  --platform=ID       Force platform (skip detection)"
    echo "  --bin-dir=DIR       Override binary directory"
    echo "  --data-dir=DIR      Override data/config directory"
    echo "  --quiet, -q         Non-interactive mode with defaults"
    echo "  --help, -h          Show this help"
    echo ""
    echo "Environment overrides:"
    echo "  B4_PLATFORM         Platform ID (generic_linux, openwrt, merlinwrt, ...)"
    echo "  B4_BIN_DIR          Binary install directory"
    echo "  B4_DATA_DIR         Data/config directory"
    echo "  B4_PKG_MANAGER      Package manager (apt, dnf, pacman, opkg, ...)"
    echo ""
    echo "Architectures:"
    echo "  amd64, 386, arm64, armv5, armv6, armv7,"
    echo "  mips, mipsle, mips_softfloat, mipsle_softfloat,"
    echo "  mips64, mips64le, loong64, ppc64, ppc64le, riscv64, s390x"
    echo ""
    echo "Examples:"
    echo "  $0                            Interactive install"
    echo "  $0 v1.4.0                     Install specific version"
    echo "  $0 --arch=mipsle_softfloat    Force architecture"
    echo "  $0 --platform=openwrt         Force platform"
    echo "  $0 --quiet                    Non-interactive with defaults"
    echo "  $0 --update                   Update to latest"
    echo "  $0 --remove                   Uninstall"
    echo "  $0 --sysinfo                  Show diagnostics"
}

main "$@"
exit 0

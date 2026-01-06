#!/bin/sh
# Detect system type and set appropriate paths
detect_system_type() {
    # Check for Entware
    # Some systems like Keenetic don't have entware_release file
    if [ -d "/opt/etc/init.d" ]; then
        # Has Entware init structure
        if [ -f "/opt/etc/entware_release" ] || [ -f "/opt/bin/opkg" ] || [ -d "/opt/lib/opkg" ]; then
            echo "entware"
            return
        fi
    fi

    # Check for Keenetic specifically
    if [ -f "/proc/device-tree/model" ] && grep -qi "keenetic" /proc/device-tree/model 2>/dev/null; then
        echo "entware"
        return
    fi

    # Fallback: if /opt/sbin exists and is writable but /etc is read-only, assume Entware-like
    if [ -d "/opt/sbin" ] && [ -w "/opt/sbin" ] && ! [ -w "/etc" ]; then
        echo "entware"
        return
    fi

    # Check for OpenWRT
    if [ -f "/etc/openwrt_release" ]; then
        echo "openwrt"
        return
    fi

    # Check for MerlinWRT
    if [ -f "/etc/merlinwrt_release" ] || [ -d "/jffs" ]; then
        echo "merlin"
        return
    fi

    # Check for standard systemd-based Linux
    if [ -d "/etc/systemd/system" ] && command_exists systemctl; then
        echo "systemd-linux"
        return
    fi

    # Check for standard init.d Linux
    if [ -d "/etc/init.d" ] && [ ! -f "/etc/openwrt_release" ]; then
        echo "sysv-linux"
        return
    fi

    # Default to generic Linux
    echo "generic-linux"
}

# Set paths based on system type
set_system_paths() {
    SYSTEM_TYPE=$(detect_system_type)

    case "$SYSTEM_TYPE" in
    entware | merlin)
        INSTALL_DIR="/opt/sbin"
        CONFIG_DIR="/opt/etc/b4"
        SERVICE_DIR="/opt/etc/init.d"
        SERVICE_NAME="S99b4"
        ;;
    openwrt)
        # OpenWRT typically uses /usr/sbin or /usr/bin
        if [ -d "/usr/sbin" ]; then
            INSTALL_DIR="/usr/sbin"
        else
            INSTALL_DIR="/usr/bin"
        fi
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/init.d"
        SERVICE_NAME="b4"
        ;;
    systemd-linux)
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/systemd/system"
        SERVICE_NAME="b4.service"
        ;;
    sysv-linux | generic-linux)
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        SERVICE_DIR="/etc/init.d"
        SERVICE_NAME="b4"
        ;;
    *)
        # Fallback
        INSTALL_DIR="/usr/local/bin"
        CONFIG_DIR="/etc/b4"
        ;;
    esac

    CONFIG_FILE="${CONFIG_DIR}/b4.json"

    print_info "Detected system: $SYSTEM_TYPE"
    print_info "Using install directory: $INSTALL_DIR"
    print_info "Using config directory: $CONFIG_DIR"
}

# Detect system architecture and return appropriate binary variant
detect_architecture() {
    arch=$(uname -m)
    arch_variant=""

    case "$arch" in
    x86_64 | amd64)
        arch_variant="amd64"
        ;;
    i386 | i486 | i586 | i686)
        arch_variant="386"
        ;;
    aarch64 | arm64)
        arch_variant="arm64"
        ;;
    armv7)
        arch_variant="armv7"
        ;;
    armv7* | armv7l | armv7-*)
        # Default to armv5 for compatibility, only use armv7 if certain
        arch_variant="armv5"

        # Only use armv7 if we have clear evidence of full support
        if [ -f /proc/cpuinfo ]; then
            # Need BOTH vfpv3+ AND proper architecture confirmation
            if grep -qE "(vfpv[3-9]|vfpv[0-9][0-9])" /proc/cpuinfo 2>/dev/null &&
                grep -qE "CPU architecture:\s*7" /proc/cpuinfo 2>/dev/null; then
                arch_variant="armv7"
                print_info "Full ARMv7 support detected, using armv7 binary"
            else
                print_warning "armv7l detected but using armv5 for compatibility (safer for routers)"
            fi
        fi
        ;;
    armv6*)
        arch_variant="armv6"
        ;;
    armv5*)
        arch_variant="armv5"
        ;;
    arm*)
        # Generic ARM - try to detect version from CPU info
        if [ -f /proc/cpuinfo ]; then
            # Look for CPU architecture line first (most reliable)
            if grep -qE "CPU architecture:\s*7" /proc/cpuinfo; then
                arch_variant="armv7"
            elif grep -qE "CPU architecture:\s*6" /proc/cpuinfo; then
                arch_variant="armv6"
            elif grep -qE "CPU architecture:\s*5" /proc/cpuinfo; then
                arch_variant="armv5"
            # Fallback to searching for ARM version strings
            elif grep -qi "ARMv7" /proc/cpuinfo; then
                arch_variant="armv7"
            elif grep -qi "ARMv6" /proc/cpuinfo; then
                arch_variant="armv6"
            elif grep -qi "ARMv5" /proc/cpuinfo; then
                arch_variant="armv5"
            else
                # Default to armv5 for maximum compatibility
                arch_variant="armv5"
            fi
        else
            # No cpuinfo available, default to safest option
            arch_variant="armv5"
        fi
        ;;
    mips64)
        # Check MIPS endianness
        if grep -qi "mips.*el\|el.*mips" /proc/cpuinfo 2>/dev/null || uname -m | grep -qi "el"; then
            arch_variant="mips64le"
        else
            arch_variant="mips64"
        fi
        # Check for softfloat (no FPU)
        if [ -f /proc/cpuinfo ]; then
            if ! grep -qi "fpu" /proc/cpuinfo 2>/dev/null || grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                arch_variant="${arch_variant}_softfloat"
                print_warning "No FPU detected, using softfloat binary"
            fi
        fi
        ;;
    mips*)
        # 32-bit MIPS - determine endianness
        if grep -qi "mips.*el\|el.*mips" /proc/cpuinfo 2>/dev/null || uname -m | grep -qi "el"; then
            arch_variant="mipsle"
        else
            arch_variant="mips"
        fi
        # Check for softfloat (no FPU)
        if [ -f /proc/cpuinfo ]; then
            if ! grep -qi "fpu" /proc/cpuinfo 2>/dev/null || grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                arch_variant="${arch_variant}_softfloat"
                print_warning "No FPU detected, using softfloat binary"
            fi
        fi
        ;;
    ppc64le)
        arch_variant="ppc64le"
        ;;
    ppc64)
        arch_variant="ppc64"
        ;;
    riscv64)
        arch_variant="riscv64"
        ;;
    s390x)
        arch_variant="s390x"
        ;;
    loongarch64)
        arch_variant="loong64"
        ;;
    *)
        print_error "Unsupported architecture: $arch"
        exit 1
        ;;
    esac

    # ONLY output the result to stdout
    echo "$arch_variant"
}

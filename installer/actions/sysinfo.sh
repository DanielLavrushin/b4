#!/bin/sh
# Action: Show system diagnostics

action_sysinfo() {
    log_header "B4 System Diagnostics"

    # --- System info ---
    log_sep
    log_detail "Hostname" "$(hostname 2>/dev/null || cat /proc/sys/kernel/hostname 2>/dev/null || echo 'unknown')"
    log_detail "Kernel" "$(uname -r)"
    log_detail "Architecture (raw)" "$(uname -m)"
    log_detail "Architecture (b4)" "$(detect_architecture 2>/dev/null || echo 'unknown')"
    [ -f /etc/os-release ] && log_detail "Distribution" "$(. /etc/os-release && echo "$PRETTY_NAME")"
    [ -f /etc/openwrt_release ] && log_detail "OpenWrt" "$(. /etc/openwrt_release && echo "$DISTRIB_DESCRIPTION")"
    is_lxc_container && log_detail "Container" "${YELLOW}LXC${NC}"

    # CPU
    cpu_cores=""
    if [ -f /proc/cpuinfo ]; then
        cpu_cores=$(grep -c "^processor" /proc/cpuinfo 2>/dev/null)
    fi
    [ -n "$cpu_cores" ] && log_detail "CPU cores" "$cpu_cores"

    # MIPS-specific diagnostics
    _raw_arch=$(uname -m)
    case "$_raw_arch" in
    mips*)
        if [ -f /proc/cpuinfo ]; then
            _cpu_model=$(grep -i "cpu model" /proc/cpuinfo 2>/dev/null | head -1 | sed 's/.*: *//')
            [ -n "$_cpu_model" ] && log_detail "CPU model" "$_cpu_model"
            if grep -qi "nofpu\|no fpu" /proc/cpuinfo 2>/dev/null; then
                log_detail "FPU" "${YELLOW}not available (softfloat needed)${NC}"
            elif grep -qi "FPU" /proc/cpuinfo 2>/dev/null; then
                log_detail "FPU" "${GREEN}available${NC}"
            fi
        fi
        if [ -f /etc/openwrt_release ]; then
            _owrt_arch=$(sed -n "s/^DISTRIB_ARCH=['\"\`]*\([^'\"\`]*\).*/\1/p" /etc/openwrt_release 2>/dev/null)
            [ -n "$_owrt_arch" ] && log_detail "OpenWrt arch" "$_owrt_arch"
        fi
        if command_exists opkg; then
            _opkg_arch=$(opkg print-architecture 2>/dev/null | grep -i "mips" | head -1 | awk '{print $2}')
            [ -n "$_opkg_arch" ] && log_detail "opkg arch" "$_opkg_arch"
        fi
        # Show ELF endianness and float from a system binary
        _elf_bin=""
        for _eb in /bin/sh /bin/busybox /bin/ls; do
            [ -f "$_eb" ] && _elf_bin="$_eb" && break
        done
        if [ -n "$_elf_bin" ]; then
            _ei_data=$(dd if="$_elf_bin" bs=1 skip=5 count=1 2>/dev/null | _byte_to_dec)
            case "$_ei_data" in
                1) log_detail "ELF endian" "little-endian" ;;
                2) log_detail "ELF endian" "big-endian" ;;
                *) ;;
            esac
        fi
        if is_softfloat; then
            log_detail "Float ABI" "${YELLOW}soft-float${NC}"
        else
            log_detail "Float ABI" "hard-float"
        fi
        ;;
    *) ;;
    esac

    # Memory
    if [ -f /proc/meminfo ]; then
        mem_total=$(awk '/^MemTotal:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        mem_avail=$(awk '/^MemAvailable:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        [ -z "$mem_avail" ] && mem_avail=$(awk '/^MemFree:/ {printf "%.0f", $2/1024}' /proc/meminfo 2>/dev/null)
        if [ -n "$mem_total" ]; then
            log_detail "Memory" "${mem_total} MB (Available: ${mem_avail:-?} MB)"
        fi
    fi

    # Platform detection (save/restore to avoid leaking into wizard)
    _saved_platform="$B4_PLATFORM"
    _saved_bin_dir="$B4_BIN_DIR"
    _saved_data_dir="$B4_DATA_DIR"
    _saved_config_file="$B4_CONFIG_FILE"
    _saved_service_type="$B4_SERVICE_TYPE"
    _saved_service_dir="$B4_SERVICE_DIR"
    _saved_service_name="$B4_SERVICE_NAME"
    _saved_pkg_manager="$B4_PKG_MANAGER"
    platform_auto_detect 2>/dev/null || true
    if [ -n "$B4_PLATFORM" ]; then
        pname=$(platform_dispatch "$B4_PLATFORM" name 2>/dev/null)
        log_detail "Detected platform" "${pname} (${B4_PLATFORM})"
        platform_call info 2>/dev/null || true
        log_detail "Binary dir" "${B4_BIN_DIR}"
        log_detail "Data dir" "${B4_DATA_DIR}"
        log_detail "Service type" "${B4_SERVICE_TYPE}"
    fi

    # --- B4 status ---
    log_sep

    found_bin=""
    _bin_crashed=""
    for dir in "$B4_BIN_DIR" /usr/local/bin /usr/bin /usr/sbin /opt/bin /opt/sbin /tmp/b4; do
        [ -z "$dir" ] && continue
        if [ -f "${dir}/${BINARY_NAME}" ] && [ -x "${dir}/${BINARY_NAME}" ]; then
            # Run in subshell to suppress kernel crash messages (e.g. "Segmentation fault")
            _ver_exit=0
            _ver_full=$(sh -c "\"${dir}/${BINARY_NAME}\" --version 2>/dev/null" 2>/dev/null) || _ver_exit=$?
            # Binary crashed (segfault=139, abort=134, etc.)
            if [ "$_ver_exit" -gt 128 ] 2>/dev/null; then
                _bin_crashed="${dir}/${BINARY_NAME}"
                continue
            fi
            if echo "$_ver_full" | grep -qi "b4 version\|bypass\|dpi"; then
                found_bin="${dir}/${BINARY_NAME}"
                _ver_out=$(echo "$_ver_full" | grep -i "version" | head -1)
                break
            fi
        fi
    done

    if [ -n "$found_bin" ]; then
        log_detail "Binary" "$found_bin"
        log_detail "Version" "$_ver_out"
        # Warn if found binary is not in the expected directory
        if [ -n "$B4_BIN_DIR" ] && [ -n "$_bin_crashed" ]; then
            log_detail "WARNING" "${RED}${_bin_crashed} crashes (segfault) — wrong architecture?${NC}"
        fi
    elif [ -n "$_bin_crashed" ]; then
        log_detail "Binary" "${_bin_crashed}"
        _arch_hint=""
        if command_exists file; then
            _arch_hint=$(file "$_bin_crashed" 2>/dev/null | sed 's/.*: //')
        elif command_exists readelf; then
            _arch_hint=$(readelf -h "$_bin_crashed" 2>/dev/null | awk '/Machine:/ {$1=""; print substr($0,2)}')
        fi
        if [ -n "$_arch_hint" ]; then
            log_detail "Status" "${RED}crashes on startup (segfault)${NC}"
            log_detail "Binary type" "$_arch_hint"
            log_detail "System arch" "$(uname -m)"
        else
            log_detail "Status" "${RED}crashes on startup (segfault) — wrong architecture?${NC}"
        fi
    else
        log_detail "Binary" "${YELLOW}not found${NC}"
    fi

    # Config file
    cfg_file=""
    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -z "$cfg" ] && continue
        [ -f "$cfg" ] && cfg_file="$cfg" && break
    done
    [ -n "$cfg_file" ] && log_detail "Config" "$cfg_file"

    # Running status + details from config and process
    if is_b4_running; then
        log_detail "Service status" "${GREEN}running${NC}"

        # Get PID and process details
        b4_pid=""
        for pf in /var/run/b4.pid /opt/var/run/b4.pid; do
            if [ -f "$pf" ] && kill -0 "$(cat "$pf")" 2>/dev/null; then
                b4_pid=$(cat "$pf")
                break
            fi
        done
        [ -z "$b4_pid" ] && b4_pid=$(pgrep -x "$BINARY_NAME" 2>/dev/null | head -1)
        [ -z "$b4_pid" ] && b4_pid=$(pgrep -f "${BINARY_NAME}" 2>/dev/null | head -1)

        if [ -n "$b4_pid" ]; then
            # Memory usage
            if [ -f "/proc/${b4_pid}/status" ]; then
                mem_kb=$(awk '/^VmRSS:/ {print $2}' "/proc/${b4_pid}/status" 2>/dev/null)
                if [ -n "$mem_kb" ]; then
                    mem_mb=$(awk "BEGIN {printf \"%.1f\", $mem_kb/1024}")
                    log_detail "Memory usage" "${mem_mb} MB (PID: ${b4_pid})"
                fi
            fi

            # Uptime
            if [ -f "/proc/${b4_pid}/stat" ]; then
                proc_start=$(awk '{print $22}' "/proc/${b4_pid}/stat" 2>/dev/null)
                clk_tck=$(getconf CLK_TCK 2>/dev/null || echo 100)
                sys_uptime=$(awk '{print int($1)}' /proc/uptime 2>/dev/null)
                if [ -n "$proc_start" ] && [ -n "$sys_uptime" ] && [ "$clk_tck" -gt 0 ] 2>/dev/null; then
                    proc_secs=$((proc_start / clk_tck))
                    running_secs=$((sys_uptime - proc_secs))
                    if [ "$running_secs" -ge 3600 ] 2>/dev/null; then
                        hours=$((running_secs / 3600))
                        mins=$(((running_secs % 3600) / 60))
                        log_detail "Uptime" "${hours}h ${mins}m"
                    elif [ "$running_secs" -ge 60 ] 2>/dev/null; then
                        mins=$((running_secs / 60))
                        log_detail "Uptime" "${mins}m"
                    elif [ "$running_secs" -ge 0 ] 2>/dev/null; then
                        log_detail "Uptime" "${running_secs}s"
                    fi
                fi
            fi
        fi
    else
        log_detail "Service status" "${YELLOW}not running${NC}"
    fi

    # Config-derived info (queue number, worker threads, geodat paths)
    if [ -n "$cfg_file" ] && command_exists jq; then
        queue_num=$(jq -r '.system.queue_num // empty' "$cfg_file" 2>/dev/null) || true
        workers=$(jq -r '.system.workers // empty' "$cfg_file" 2>/dev/null) || true
        geosite=$(jq -r '.system.geo.sitedat_path // empty' "$cfg_file" 2>/dev/null) || true
        geoip=$(jq -r '.system.geo.ipdat_path // empty' "$cfg_file" 2>/dev/null) || true

        [ -n "$queue_num" ] && [ "$queue_num" != "null" ] && log_detail "Queue number" "$queue_num"
        [ -n "$workers" ] && [ "$workers" != "null" ] && log_detail "Worker threads" "$workers"

        if [ -n "$geosite" ] && [ "$geosite" != "null" ] && [ -f "$geosite" ]; then
            size=$(ls -lh "$geosite" 2>/dev/null | awk '{print $5}')
            log_detail "geosite.dat" "${geosite} (${size})"
        fi
        if [ -n "$geoip" ] && [ "$geoip" != "null" ] && [ -f "$geoip" ]; then
            size=$(ls -lh "$geoip" 2>/dev/null | awk '{print $5}')
            log_detail "geoip.dat" "${geoip} (${size})"
        fi
    fi

    log_sep

    # --- Kernel modules ---
    echo ""
    log_info "Kernel modules:"
    for mod in xt_NFQUEUE nfnetlink_queue xt_connbytes xt_multiport nf_conntrack; do
        if lsmod 2>/dev/null | grep -q "^${mod}"; then
            printf "    ${GREEN}loaded${NC}   %s\n" "$mod" >&2
        elif _kmod_builtin "$mod"; then
            printf "    ${GREEN}built-in${NC} %s\n" "$mod" >&2
        else
            printf "    ${YELLOW}missing${NC}  %s ${DIM}(may be built-in)${NC}\n" "$mod" >&2
        fi
    done

    # Functional test — does NFQUEUE actually work?
    _nfq_ipt=""
    if command_exists iptables; then
        _nfq_ipt="iptables"
    elif command_exists iptables-legacy; then
        _nfq_ipt="iptables-legacy"
    fi
    if [ -n "$_nfq_ipt" ]; then
        if $_nfq_ipt -t mangle -C B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null; then
            $_nfq_ipt -t mangle -D B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null || true
        fi
        if $_nfq_ipt -t mangle -N B4_TEST 2>/dev/null; then
            if $_nfq_ipt -t mangle -A B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null; then
                printf "    ${GREEN}  OK${NC}    %s\n" "NFQUEUE works (functional test passed)" >&2
                $_nfq_ipt -t mangle -D B4_TEST -j NFQUEUE --queue-num 0 2>/dev/null || true
            else
                printf "    ${RED}  FAIL${NC}  %s\n" "NFQUEUE not functional" >&2
            fi
            $_nfq_ipt -t mangle -X B4_TEST 2>/dev/null || true
        fi
        if $_nfq_ipt -t filter -N B4_CB_TEST 2>/dev/null; then
            if $_nfq_ipt -t filter -A B4_CB_TEST -p tcp -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 0:10 -j ACCEPT 2>/dev/null; then
                printf "    ${GREEN}  OK${NC}    %s\n" "connbytes works (functional test passed)" >&2
            else
                printf "    ${RED}  FAIL${NC}  %s\n" "connbytes not functional" >&2
            fi
            $_nfq_ipt -t filter -F B4_CB_TEST 2>/dev/null || true
            $_nfq_ipt -t filter -X B4_CB_TEST 2>/dev/null || true
        fi
    fi

    # --- Tools & dependencies ---
    echo ""
    log_info "Required tools:"
    # Firewall detection with functional nftables test
    _fw_found=0
    if command_exists nft; then
        if nft add table inet _b4_test 2>/dev/null; then
            nft delete table inet _b4_test 2>/dev/null || true
            printf "    ${GREEN}found${NC}   nft ${DIM}(nftables — functional)${NC}\n" >&2
            _fw_found=1
        else
            printf "    ${YELLOW}found${NC}   nft ${DIM}(nftables — ${RED}not functional${NC}${DIM})${NC}\n" >&2
        fi
    fi
    if command_exists iptables; then
        _ipt_ver=$(iptables --version 2>/dev/null)
        if echo "$_ipt_ver" | grep -q "nf_tables"; then
            printf "    ${YELLOW}found${NC}   iptables ${DIM}(nft-variant)${NC}\n" >&2
        else
            printf "    ${GREEN}found${NC}   iptables\n" >&2
        fi
        _fw_found=1
    fi
    if command_exists iptables-legacy; then
        printf "    ${GREEN}found${NC}   iptables-legacy\n" >&2
        _fw_found=1
    fi
    if [ "$_fw_found" = "0" ]; then
        printf "    ${RED}missing${NC} iptables or nft ${DIM}(firewall required)${NC}\n" >&2
    fi
    # Archive
    for tool in tar; do
        if command_exists "$tool"; then
            printf "    ${GREEN}found${NC}   %s\n" "$tool" >&2
        else
            printf "    ${RED}missing${NC} %s ${DIM}(required for install)${NC}\n" >&2
        fi
    done
    # curl/wget with HTTPS check
    if command_exists curl; then
        if curl -sI --max-time 5 "https://github.com" >/dev/null 2>&1; then
            printf "    ${GREEN}found${NC}   curl ${GREEN}(HTTPS OK)${NC}\n" >&2
        else
            printf "    ${YELLOW}found${NC}   curl ${RED}(HTTPS failed)${NC}\n" >&2
        fi
    elif command_exists wget; then
        if wget --spider -q --timeout=5 "https://github.com" 2>/dev/null; then
            printf "    ${GREEN}found${NC}   wget ${GREEN}(HTTPS OK)${NC}\n" >&2
        elif wget --spider -q --timeout=5 --no-check-certificate "https://github.com" 2>/dev/null; then
            printf "    ${YELLOW}found${NC}   wget ${YELLOW}(HTTPS only with --no-check-certificate)${NC}\n" >&2
        else
            printf "    ${YELLOW}found${NC}   wget ${RED}(HTTPS failed)${NC}\n" >&2
        fi
    else
        printf "    ${RED}missing${NC} curl or wget ${DIM}(required for download)${NC}\n" >&2
    fi

    echo ""
    log_info "Optional tools:"
    for tool in jq sha256sum nohup modprobe ipset; do
        if command_exists "$tool"; then
            printf "    ${GREEN}found${NC}   %s\n" "$tool" >&2
        else
            case "$tool" in
            jq)        printf "    ${YELLOW}missing${NC} %s ${DIM}(config editing won't work)${NC}\n" "$tool" >&2 ;;
            sha256sum) printf "    ${YELLOW}missing${NC} %s ${DIM}(checksum verify skipped)${NC}\n" "$tool" >&2 ;;
            nohup)     printf "    ${YELLOW}missing${NC} %s ${DIM}(service may stop on session close)${NC}\n" "$tool" >&2 ;;
            modprobe)  printf "    ${YELLOW}missing${NC} %s ${DIM}(kernel modules loaded via insmod)${NC}\n" "$tool" >&2 ;;
            ipset)
                if _nft_functional; then
                    printf "    ${YELLOW}missing${NC} %s ${DIM}(needed for routing on iptables systems)${NC}\n" "$tool" >&2
                elif command_exists iptables || command_exists iptables-legacy; then
                    printf "    ${RED}missing${NC} %s ${DIM}(required — iptables backend in use, install ipset)${NC}\n" "$tool" >&2
                else
                    printf "    ${YELLOW}missing${NC} %s ${DIM}(no firewall backend detected — backend availability unclear)${NC}\n" "$tool" >&2
                fi
                ;;
            *) ;;
            esac
        fi
    done
    # Show secondary download tool if primary exists
    if command_exists curl && command_exists wget; then
        printf "    ${GREEN}found${NC}   wget ${DIM}(fallback downloader)${NC}\n" >&2
    elif command_exists wget && ! command_exists curl; then
        printf "    ${YELLOW}missing${NC} curl ${DIM}(wget used as primary)${NC}\n" >&2
    elif command_exists curl && ! command_exists wget; then
        printf "    ${DIM}  ---${NC}   wget ${DIM}(not needed, curl available)${NC}\n" >&2
    fi

    # Package manager
    echo ""
    detect_pkg_manager
    log_detail "Package manager" "${B4_PKG_MANAGER:-none}"

    # --- Storage ---
    echo ""
    log_info "Storage:"
    _sysinfo_shown_devs=""
    for dir in / /opt /tmp /jffs /mnt/sda1 /etc/storage; do
        if [ -d "$dir" ]; then
            _sysinfo_show_storage "$dir"
        fi
    done
    # Auto-discover mounted USB/external storage under /mnt
    for dir in /mnt/*; do
        [ -d "$dir" ] || continue
        # Skip already-shown entries
        case "$dir" in /mnt/sda1) continue ;; *) ;; esac
        _sysinfo_show_storage "$dir"
    done

    echo ""
    log_sep

    # Restore globals so sysinfo doesn't leak into wizard
    B4_PLATFORM="$_saved_platform"
    B4_BIN_DIR="$_saved_bin_dir"
    B4_DATA_DIR="$_saved_data_dir"
    B4_CONFIG_FILE="$_saved_config_file"
    B4_SERVICE_TYPE="$_saved_service_type"
    B4_SERVICE_DIR="$_saved_service_dir"
    B4_SERVICE_NAME="$_saved_service_name"
    B4_PKG_MANAGER="$_saved_pkg_manager"
}

_sysinfo_show_storage() {
    _dir="$1"
    # Get underlying device to avoid showing the same filesystem twice
    _dev=$(df "$_dir" 2>/dev/null | tail -1 | awk '{print $1}')
    case "$_sysinfo_shown_devs" in
    *"|${_dev}|"*) return 0 ;; # already shown
    esac
    _sysinfo_shown_devs="${_sysinfo_shown_devs}|${_dev}|"
    avail=$(df -h "$_dir" 2>/dev/null | tail -1 | awk '{print $4}')
    writable="rw"
    [ ! -w "$_dir" ] && writable="ro"
    printf "    %-20s %s available (%s)\n" "$_dir" "${avail:-?}" "$writable" >&2
}

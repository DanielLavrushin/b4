#!/bin/sh
# Feature: GeoSite data (geosite.dat)
# Downloads v2ray-format geosite database for domain categorization

GEOSITE_SOURCES="1|Loyalsoldier|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download|11
2|RUNET Freedom|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release|74"

feature_geosite_name() {
    echo "GeoSite data"
}

feature_geosite_description() {
    echo "Download geosite.dat for domain categorization"
}

feature_geosite_default_enabled() {
    echo "yes"
}

feature_geosite_prepare() {
    _geo_prepare geosite
}

feature_geosite_run() {
    _geo_commit geosite
}

feature_geosite_remove() {
    _geo_remove_file "sitedat_path" "geosite.dat"
}

register_feature "geosite"

# --- Shared helpers used by both geosite and geoip features ---

_geo_kind() {
    case "$1" in
    geosite)
        _gk_sources="$GEOSITE_SOURCES"
        _gk_default=2
        _gk_path_key=sitedat_path
        _gk_url_key=sitedat_url
        _gk_label=GeoSite
        ;;
    geoip)
        _gk_sources="$GEOIP_SOURCES"
        _gk_default=3
        _gk_path_key=ipdat_path
        _gk_url_key=ipdat_url
        _gk_label=GeoIP
        ;;
    *) return 1 ;;
    esac
    _gk_file="${1}.dat"
}

_geo_state_set() {
    eval "_GEO_STATE_${1}=\$2"
    eval "_GEO_TARGET_${1}=\$3"
    eval "_GEO_CFG_${1}=\$4"
    eval "_GEO_SRC_${1}=\$5"
}

_geo_state_get() {
    eval "_gs_state=\${_GEO_STATE_${1}:-}"
    eval "_gs_target=\${_GEO_TARGET_${1}:-}"
    eval "_gs_cfg=\${_GEO_CFG_${1}:-}"
    eval "_gs_src=\${_GEO_SRC_${1}:-}"
}

_geo_sum_parse() {
    _gsp_sum=$(tr -d '\r' | awk 'NR == 1 { print tolower($1) }')
    case "$_gsp_sum" in
    '' | *[!0-9a-f]*) return 1 ;;
    esac
    [ "${#_gsp_sum}" -eq 64 ] || return 1
    echo "$_gsp_sum"
}

_geodat_header_ok() {
    dd if="$1" bs=8 count=1 2>/dev/null | od -b | head -1 | awk '
        function oct(s,  i, v) { v = 0; for (i = 1; i <= length(s); i++) v = v * 8 + substr(s, i, 1); return v }
        {
            for (i = 2; i <= NF; i++) b[n++] = oct($i)
            if (n < 3 || b[0] != 10) exit 1
            for (i = 1; i < n && i <= 5; i++) if (b[i] < 128) break
            if (i >= n - 1 || i > 5) exit 1
            exit (b[i + 1] == 10) ? 0 : 1
        }
        END { if (n == 0) exit 1 }'
}

_geodat_verify() {
    _gv_file="$1"
    _gv_url="$2"
    _gv_orig="$3"
    _gv_name=$(basename "$_gv_orig")

    if ! _geodat_header_ok "$_gv_file"; then
        log_warn "The server did not return a geodata file for ${_gv_name} (an error or block page?)"
        return 1
    fi

    _gv_second=0
    _gv_want=$(_do_fetch_stdout "${_gv_url}.sha256sum" | _geo_sum_parse) || _gv_want=""
    if [ "$_gv_url" != "$_gv_orig" ]; then
        if [ -n "$_gv_want" ]; then
            _gv_second=1
        else
            _gv_want=$(fetch_stdout "${_gv_orig}.sha256sum" | _geo_sum_parse) || _gv_want=""
        fi
    fi

    if [ -z "$_gv_want" ]; then
        log_warn "No checksum is published for ${_gv_name}, accepting it unverified"
        return 0
    fi

    _gv_have=$(sha256_of "$_gv_file") || {
        log_warn "No working sha256 tool found, accepting ${_gv_name} unverified"
        return 0
    }
    [ "$_gv_have" = "$_gv_want" ] && return 0

    if [ "$_gv_second" -eq 1 ]; then
        _gv_alt=$(fetch_stdout "${_gv_orig}.sha256sum" | _geo_sum_parse) || _gv_alt=""
        if [ -n "$_gv_alt" ] && [ "$_gv_alt" != "$_gv_want" ]; then
            [ "$_gv_have" = "$_gv_alt" ] && return 0
            _gv_want="${_gv_want} or ${_gv_alt}"
        fi
    fi

    log_warn "Checksum mismatch for ${_gv_name}: published ${_gv_want}, got ${_gv_have}"
    return 1
}

_geo_kept_notice() {
    if [ -f "$2" ]; then
        log_err "The current ${1}.dat is kept"
    else
        log_warn "b4 skips ${1} categories until ${1}.dat is downloaded"
    fi
}

_geo_mb() {
    awk -v b="$1" 'BEGIN { printf "%.1f MB", b / 1048576 }'
}

_geo_existing_dir() {
    _ged_dir="$1"
    while [ -n "$_ged_dir" ] && [ "$_ged_dir" != "/" ] && [ ! -d "$_ged_dir" ]; do
        _ged_dir=$(dirname "$_ged_dir")
    done
    echo "${_ged_dir:-/}"
}

_geo_source_field() {
    echo "$_gk_sources" | awk -F'|' -v n="$1" -v f="$2" '$1 == n { print $f; exit }'
}

_geo_probe_sizes() {
    _gp_sizes="${TEMP_DIR:-/tmp}/geo_sizes_${_gp_kind}_$$"
    rm -rf "$_gp_sizes" 2>/dev/null || true
    mkdir -p "$_gp_sizes" 2>/dev/null || return 0
    echo "$_gk_sources" | {
        while IFS='|' read -r _gps_num _gps_name _gps_base _gps_approx; do
            [ -n "$_gps_num" ] || continue
            (remote_size "${_gps_base}/${_gk_file}" "$B4_PROBE_TIMEOUT" >"${_gp_sizes}/${_gps_num}" 2>/dev/null || true) &
        done
        wait
    }
}

_geo_source_size() {
    _gss_live=$(cat "${_gp_sizes}/$1" 2>/dev/null) || _gss_live=""
    case "$_gss_live" in
    '' | *[!0-9]*) ;;
    *)
        echo "$_gss_live live"
        return 0
        ;;
    esac
    _gss_mb=$(_geo_source_field "$1" 4)
    case "$_gss_mb" in
    '' | *[!0-9]*) ;;
    *) echo "$((_gss_mb * 1048576)) approx" ;;
    esac
}

_geo_source_bytes() {
    _geo_source_size "$1" | cut -d' ' -f1
}

_geo_size_text() {
    _gst_size=$(_geo_source_size "$1")
    case "$_gst_size" in
    *live) _geo_mb "${_gst_size% live}" ;;
    *approx) echo "~$(_geo_source_field "$1" 4) MB" ;;
    esac
}

_geo_source_label() {
    _gsl_label=$(_geo_source_field "$1" 2)
    _gsl_size=$(_geo_size_text "$1")
    if [ -n "$_gsl_size" ]; then
        _gsl_label="${_gsl_label}, ${_gsl_size}"
    fi
    if [ "$1" = "$_gk_default" ]; then
        _gsl_label="${_gsl_label}, recommended"
    fi
    echo "$_gsl_label"
}

_geo_fits() {
    case "$1" in
    '' | *[!0-9]*) return 0 ;;
    esac
    case "$_gp_avail" in
    '' | *[!0-9]*) return 0 ;;
    esac
    [ "$_gp_avail" -ge $((($1 + 1023) / 1024 + 1024)) ]
}

_geo_pick_default() {
    _gp_pick="$_gk_default"
    _gpd_bytes=$(_geo_source_bytes "$_gk_default")
    if _geo_fits "$_gpd_bytes"; then
        return 0
    fi

    _gpd_best=""
    _gpd_best_bytes=""
    for _gpd_num in $(echo "$_gk_sources" | cut -d'|' -f1); do
        _gpd_size=$(_geo_source_bytes "$_gpd_num")
        case "$_gpd_size" in
        '' | *[!0-9]*) continue ;;
        esac
        _geo_fits "$_gpd_size" || continue
        if [ -z "$_gpd_best" ] || [ "$_gpd_size" -lt "$_gpd_best_bytes" ]; then
            _gpd_best="$_gpd_num"
            _gpd_best_bytes="$_gpd_size"
        fi
    done
    if [ -z "$_gpd_best" ]; then
        return 0
    fi

    _gp_pick="$_gpd_best"
    _gp_note="$(_geo_source_field "$_gk_default" 2) ($(_geo_size_text "$_gk_default")) does not fit in ${_gp_dir} ($(_geo_mb $((_gp_avail * 1024))) free), so the default is $(_geo_source_field "$_gpd_best" 2) ($(_geo_size_text "$_gpd_best"))"
}

_geo_smaller_hint() {
    [ -n "$_gp_sizes" ] || return 0
    _gp_avail=$(get_avail_kb "$(dirname "$_gp_target")") || _gp_avail=""
    for _gsh_num in $(echo "$_gk_sources" | cut -d'|' -f1); do
        [ "$_gsh_num" != "$_gp_pick" ] || continue
        _gsh_bytes=$(_geo_source_bytes "$_gsh_num")
        case "$_gsh_bytes" in
        '' | *[!0-9]*) continue ;;
        esac
        if _geo_fits "$_gsh_bytes"; then
            log_info "$(_geo_source_field "$_gsh_num" 2) ($(_geo_size_text "$_gsh_num")) fits in the free space"
        fi
    done
}

_geo_room_ok() {
    _gr_target="$1"
    _gr_src="$2"
    _gr_bytes="${3:-}"
    _gr_dir=$(dirname "$_gr_target")

    case "$_gr_bytes" in
    -) _gr_bytes="" ;;
    '') _gr_bytes=$(remote_size "$_gr_src") || _gr_bytes="" ;;
    esac
    if [ -z "$_gr_bytes" ] && [ -f "$_gr_target" ]; then
        _gr_bytes=$(wc -c 2>/dev/null <"$_gr_target" | awk '{print $1}')
    fi
    case "$_gr_bytes" in
    '' | *[!0-9]*) return 0 ;;
    esac

    _gr_avail=$(get_avail_kb "$_gr_dir")
    case "$_gr_avail" in
    '' | *[!0-9]*) return 0 ;;
    esac

    _gr_need=$(((_gr_bytes + 1023) / 1024 + 1024))
    _gr_for=""
    _gr_bin="${TEMP_DIR}/${BINARY_NAME}"
    if [ -f "$_gr_bin" ] && [ -d "$B4_BIN_DIR" ] &&
        same_filesystem "$_gr_dir" "$B4_BIN_DIR" && ! same_filesystem "$TEMP_DIR" "$B4_BIN_DIR"; then
        _gr_binkb=$(du -k "$_gr_bin" 2>/dev/null | awk '{print $1}')
        case "$_gr_binkb" in
        '' | *[!0-9]*) ;;
        *)
            _gr_need=$((_gr_need + _gr_binkb + BIN_SLACK_KB))
            _gr_for=", including room for the new b4 binary"
            ;;
        esac
    fi
    [ "$_gr_avail" -ge "$_gr_need" ] && return 0
    log_err "Not enough space in ${_gr_dir}: ${_gr_avail}KB free, ${_gr_need}KB needed${_gr_for}"
    return 1
}

_geo_prepare() {
    _gp_kind="$1"
    _geo_kind "$_gp_kind" || return 1
    _geo_state_set "$_gp_kind" failed "" "" ""

    _gp_dir="$B4_DATA_DIR"
    _gp_current=""

    if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
        _gp_existing=$(jq -r ".system.geo.${_gk_path_key} // empty" "$B4_CONFIG_FILE" 2>/dev/null) || true
        if [ -n "$_gp_existing" ] && [ "$_gp_existing" != "null" ]; then
            if is_abs_path "$_gp_existing"; then
                _gp_dir=$(dirname "$_gp_existing")
                log_info "Found existing ${_gp_kind} path: $_gp_dir"
            else
                log_warn "Ignoring non-absolute ${_gp_kind} path in config: $_gp_existing"
            fi
        fi
        _gp_current=$(jq -r ".system.geo.${_gk_url_key} // empty" "$B4_CONFIG_FILE" 2>/dev/null) || true
        [ "$_gp_current" != "null" ] || _gp_current=""
    fi

    _gp_pick=""
    _gp_custom=""
    if [ -n "$_gp_current" ]; then
        _gp_pick=$(echo "$_gk_sources" | awk -F'|' -v u="$_gp_current" -v f="$_gk_file" '$3 "/" f == u { print $1; exit }')
        if [ -z "$_gp_pick" ]; then
            _gp_custom=$(($(echo "$_gk_sources" | grep -c '|') + 1))
            _gp_pick="$_gp_custom"
        fi
    fi

    _gp_sizes=""
    _gp_avail=""
    _gp_note=""
    if [ "$QUIET_MODE" -ne 1 ] || [ -z "$_gp_pick" ]; then
        log_info "Checking the size of each ${_gk_label} source..."
        _geo_probe_sizes
        _gp_avail=$(get_avail_kb "$(_geo_existing_dir "$_gp_dir")") || _gp_avail=""
    fi
    if [ -z "$_gp_pick" ]; then
        _geo_pick_default
        if [ -n "$_gp_note" ] && [ "$QUIET_MODE" -eq 1 ]; then
            log_warn "$_gp_note"
        fi
    fi

    if [ "$QUIET_MODE" -ne 1 ]; then
        log_sep
        echo ""

        echo "  Available ${_gp_kind} sources:"
        for _gp_num in $(echo "$_gk_sources" | cut -d'|' -f1); do
            _gp_line=$(_geo_source_label "$_gp_num")
            if [ -n "$_gp_current" ] && [ "$_gp_num" = "$_gp_pick" ]; then
                _gp_line="${_gp_line}, in use"
            fi
            printf "    ${BOLD}%s${NC}) %s\n" "$_gp_num" "$_gp_line"
        done
        if [ -n "$_gp_custom" ]; then
            printf "    ${BOLD}%s${NC}) %s\n" "$_gp_custom" "Source in use: ${_gp_current}"
        fi
        echo ""
        if [ -n "$_gp_note" ]; then
            log_warn "$_gp_note"
        fi

        read_input "Select source [${_gp_pick}]: " "$_gp_pick"

        if [ -n "$_gp_custom" ] && [ "$_INPUT" = "$_gp_custom" ]; then
            _gp_pick="$_gp_custom"
        elif [ -n "$(_geo_source_field "$_INPUT" 3)" ]; then
            _gp_pick="$_INPUT"
        else
            log_warn "Invalid selection, using default"
        fi
    fi

    _gp_known=""
    if [ -n "$_gp_custom" ] && [ "$_gp_pick" = "$_gp_custom" ]; then
        _gp_src="$_gp_current"
    else
        _gp_src="$(_geo_source_field "$_gp_pick" 3)/${_gk_file}"
        case "$(_geo_source_size "$_gp_pick")" in
        *live) _gp_known=$(_geo_source_bytes "$_gp_pick") ;;
        *) [ -z "$_gp_sizes" ] || _gp_known="-" ;;
        esac
    fi

    if [ "$QUIET_MODE" -ne 1 ]; then
        while true; do
            read_input "Save directory [${_gp_dir}]: " "$_gp_dir"
            if is_abs_path "$_INPUT"; then
                _gp_dir="$_INPUT"
                break
            fi
            log_warn "Save directory must be an absolute path (got: ${_INPUT:-empty})"
        done
    fi

    if ! is_abs_path "$_gp_dir"; then
        log_err "${_gk_label} save directory must be an absolute path (got: ${_gp_dir:-empty})"
        return 1
    fi

    ensure_dir "$_gp_dir" "${_gk_label} directory" || return 1

    _gp_cfg="${_gp_dir}/${_gk_file}"
    _gp_target="$_gp_cfg"
    if [ -L "$_gp_target" ]; then
        _gp_real=$(readlink -f "$_gp_target" 2>/dev/null) || _gp_real=""
        [ -n "$_gp_real" ] && _gp_target="$_gp_real"
    fi
    _gp_new="${_gp_target}.new"
    rm -f "$_gp_new" "${_gp_new}.part" 2>/dev/null || true

    _gp_want=$(fetch_stdout "${_gp_src}.sha256sum" | _geo_sum_parse) || _gp_want=""
    if [ -n "$_gp_want" ] && [ -f "$_gp_target" ]; then
        _gp_have=$(sha256_of "$_gp_target") || _gp_have=""
        if [ "$_gp_have" = "$_gp_want" ]; then
            log_ok "${_gk_file} is up to date"
            _geo_state_set "$_gp_kind" fresh "$_gp_target" "$_gp_cfg" "$_gp_src"
            return 0
        fi
    fi

    if ! _geo_room_ok "$_gp_target" "$_gp_src" "$_gp_known"; then
        _geo_smaller_hint
        _geo_kept_notice "$_gp_kind" "$_gp_target"
        return 1
    fi

    log_info "Downloading ${_gk_file}..."
    pending_add "$_gp_new"
    B4_FETCH_MAX_TIME="$B4_GEO_MAX_TIME"
    _gp_rc=0
    fetch_file "$_gp_src" "$_gp_new" _geodat_verify || _gp_rc=$?
    B4_FETCH_MAX_TIME=""
    if [ "$_gp_rc" -ne 0 ]; then
        rm -f "$_gp_new" 2>/dev/null || true
        pending_drop "$_gp_new"
        log_err "Failed to download ${_gk_file}"
        _geo_kept_notice "$_gp_kind" "$_gp_target"
        return 1
    fi

    log_ok "${_gk_file} downloaded"
    _geo_state_set "$_gp_kind" pending "$_gp_target" "$_gp_cfg" "$_gp_src"
    return 0
}

_geo_commit() {
    _gc_kind="$1"
    _geo_kind "$_gc_kind" || return 1
    _geo_state_get "$_gc_kind"
    if [ -z "$_gs_state" ]; then
        _geo_prepare "$_gc_kind" || true
        _geo_state_get "$_gc_kind"
    fi

    case "$_gs_state" in
    pending)
        _gc_new="${_gs_target}.new"
        if ! mv -f "$_gc_new" "$_gs_target" 2>/dev/null; then
            rm -f "$_gc_new" 2>/dev/null || true
            pending_drop "$_gc_new"
            log_err "Could not replace ${_gs_target}"
            _geo_kept_notice "$_gc_kind" "$_gs_target"
            _geo_state_set "$_gc_kind" failed "" "" ""
            return 1
        fi
        flush_disk
        pending_drop "$_gc_new"
        log_ok "${_gk_file} installed to $(dirname "$_gs_cfg")"
        _geo_state_set "$_gc_kind" fresh "$_gs_target" "$_gs_cfg" "$_gs_src"
        ;;
    fresh) ;;
    *) return 1 ;;
    esac

    _geo_update_config "$_gk_path_key" "$_gs_cfg" "$_gk_url_key" "$_gs_src"
}

_geo_update_config() {
    path_key="$1"
    path_val="$2"
    url_key="$3"
    url_val="$4"

    if ! command_exists jq; then
        log_warn "jq not found — please update config manually:"
        log_info "  Set system.geo.${path_key} = ${path_val}"
        return 0
    fi

    if [ ! -f "$B4_CONFIG_FILE" ]; then
        # Create minimal config with just this geo key
        (umask 077 && jq -n \
            --arg pv "$path_val" \
            --arg uv "$url_val" \
            "{ system: { geo: { ${path_key}: \$pv, ${url_key}: \$uv } } }" \
            >"$B4_CONFIG_FILE")
        config_secure_perms
        log_ok "Created config with ${path_key}"
        return 0
    fi

    # Update existing config — merge into system.geo, preserving other keys
    tmp="${B4_CONFIG_FILE}.tmp"
    if (umask 077 && jq \
        --arg pv "$path_val" \
        --arg uv "$url_val" \
        ".system.geo = (.system.geo // {}) + { \"${path_key}\": \$pv, \"${url_key}\": \$uv }" \
        "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null); then
        mv "$tmp" "$B4_CONFIG_FILE"
        config_secure_perms
        log_ok "Config updated: ${path_key}"
    else
        rm -f "$tmp"
        log_warn "Failed to update config, please set ${path_key} manually"
    fi
}

# Find the path of a geodata file without removing it
# Usage: _geo_find_file_path "geoip" or _geo_find_file_path "geosite"
_geo_find_file_path() {
    _feat="$1"
    case "$_feat" in
    geoip)   _cfg_key="ipdat_path";   _fname="geoip.dat" ;;
    geosite) _cfg_key="sitedat_path"; _fname="geosite.dat" ;;
    *) return 1 ;;
    esac

    # Try reading path from config
    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -f "$cfg" ] || continue
        if command_exists jq; then
            fpath=$(jq -r ".system.geo.${_cfg_key} // empty" "$cfg" 2>/dev/null) || true
            if [ -n "$fpath" ] && [ -f "$fpath" ]; then
                echo "$fpath"
                return 0
            fi
        fi
    done

    # Fallback: check default locations
    for dir in /etc/b4 /opt/etc/b4 "$B4_DATA_DIR"; do
        [ -z "$dir" ] && continue
        if [ -f "${dir}/${_fname}" ]; then
            echo "${dir}/${_fname}"
            return 0
        fi
    done
}

_geo_remove_file() {
    config_key="$1"
    filename="$2"

    # Try reading path from config
    for cfg in "$B4_CONFIG_FILE" /etc/b4/b4.json /opt/etc/b4/b4.json; do
        [ -f "$cfg" ] || continue
        if command_exists jq; then
            fpath=$(jq -r ".system.geo.${config_key} // empty" "$cfg" 2>/dev/null) || true
            if [ -n "$fpath" ] && [ -f "$fpath" ]; then
                log_info "Found ${filename}: ${fpath}"
                if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove ${filename}?" "y"; then
                    rm -f "$fpath" && log_info "Removed: $fpath"
                else
                    log_info "Keeping ${filename}"
                fi
                return 0
            fi
        fi
    done

    # Fallback: check default locations
    for dir in /etc/b4 /opt/etc/b4; do
        if [ -f "${dir}/${filename}" ]; then
            log_info "Found ${filename}: ${dir}/${filename}"
            if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove ${filename}?" "y"; then
                rm -f "${dir}/${filename}" && log_info "Removed: ${dir}/${filename}"
            else
                log_info "Keeping ${filename}"
            fi
            return 0
        fi
    done
}

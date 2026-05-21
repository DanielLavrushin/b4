#!/bin/sh
# Feature: GeoSite data (geosite.dat)
# Downloads v2ray-format geosite database for domain categorization

GEOSITE_SOURCES="1|Loyalsoldier|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom (recommended)|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release"

feature_geosite_name() {
    echo "GeoSite data"
}

feature_geosite_description() {
    echo "Download geosite.dat for domain categorization"
}

feature_geosite_default_enabled() {
    echo "yes"
}

feature_geosite_run() {
    base_url=$(echo "$GEOSITE_SOURCES" | grep "^2|" | cut -d'|' -f3)
    save_dir="$B4_DATA_DIR"

    if [ "$QUIET_MODE" -ne 1 ]; then
        log_sep
        echo ""

        echo "  Available geosite sources:"
        echo "$GEOSITE_SOURCES" | while IFS='|' read -r num name _url; do
            [ -n "$num" ] && printf "    ${BOLD}%s${NC}) %s\n" "$num" "$name"
        done
        echo ""

        read_input "Select source [2]: " "2"

        _sel_url=$(echo "$GEOSITE_SOURCES" | grep "^${_INPUT}|" | cut -d'|' -f3) || true
        [ -n "$_sel_url" ] && base_url="$_sel_url" || log_warn "Invalid selection, using default"

        if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
            existing=$(jq -r '.system.geo.sitedat_path // empty' "$B4_CONFIG_FILE" 2>/dev/null) || true
            if [ -n "$existing" ] && [ "$existing" != "null" ]; then
                if is_abs_path "$existing"; then
                    save_dir=$(dirname "$existing")
                    log_info "Found existing geosite path: $save_dir"
                else
                    log_warn "Ignoring non-absolute geosite path in config: $existing"
                fi
            fi
        fi

        while true; do
            read_input "Save directory [${save_dir}]: " "$save_dir"
            if is_abs_path "$_INPUT"; then
                save_dir="$_INPUT"
                break
            fi
            log_warn "Save directory must be an absolute path (got: ${_INPUT:-empty})"
        done
    fi

    if ! is_abs_path "$save_dir"; then
        log_err "Geosite save directory must be an absolute path (got: ${save_dir:-empty})"
        return 1
    fi

    ensure_dir "$save_dir" "GeoSite directory" || return 1

    # Download
    log_info "Downloading geosite.dat..."
    if ! fetch_file "${base_url}/geosite.dat" "${save_dir}/geosite.dat"; then
        log_err "Failed to download geosite.dat"
        return 1
    fi
    [ ! -s "${save_dir}/geosite.dat" ] && log_err "geosite.dat is empty" && return 1

    log_ok "geosite.dat downloaded to ${save_dir}"

    # Update config
    _geo_update_config "sitedat_path" "${save_dir}/geosite.dat" "sitedat_url" "${base_url}/geosite.dat"
}

feature_geosite_remove() {
    _geo_remove_file "sitedat_path" "geosite.dat"
}

register_feature "geosite"

# --- Shared helpers used by both geosite and geoip features ---

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
        jq -n \
            --arg pv "$path_val" \
            --arg uv "$url_val" \
            "{ system: { geo: { ${path_key}: \$pv, ${url_key}: \$uv } } }" \
            >"$B4_CONFIG_FILE"
        log_ok "Created config with ${path_key}"
        return 0
    fi

    # Update existing config — merge into system.geo, preserving other keys
    tmp="${B4_CONFIG_FILE}.tmp"
    if jq \
        --arg pv "$path_val" \
        --arg uv "$url_val" \
        ".system.geo = (.system.geo // {}) + { \"${path_key}\": \$pv, \"${url_key}\": \$uv }" \
        "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null; then
        mv "$tmp" "$B4_CONFIG_FILE"
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

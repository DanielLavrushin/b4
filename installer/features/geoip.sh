#!/bin/sh
# Feature: GeoIP data (geoip.dat)
# Downloads v2ray-format geoip database for IP-based filtering

GEOIP_SOURCES="1|Loyalsoldier|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release
3|B4 GeoIP (recommended)|https://github.com/DanielLavrushin/b4geoip/releases/latest/download"

feature_geoip_name() {
    echo "GeoIP data"
}

feature_geoip_description() {
    echo "Download geoip.dat for IP-based filtering"
}

feature_geoip_default_enabled() {
    echo "yes"
}

feature_geoip_run() {
    base_url=$(echo "$GEOIP_SOURCES" | grep "^3|" | cut -d'|' -f3)
    save_dir="$B4_DATA_DIR"

    if [ "$QUIET_MODE" -ne 1 ]; then
        log_sep
        echo ""

        echo "  Available geoip sources:"
        echo "$GEOIP_SOURCES" | while IFS='|' read -r num name _url; do
            [ -n "$num" ] && printf "    ${BOLD}%s${NC}) %s\n" "$num" "$name"
        done
        echo ""

        read_input "Select source [3]: " "3"

        _sel_url=$(echo "$GEOIP_SOURCES" | grep "^${_INPUT}|" | cut -d'|' -f3) || true
        [ -n "$_sel_url" ] && base_url="$_sel_url" || log_warn "Invalid selection, using default"

        if [ -f "$B4_CONFIG_FILE" ] && command_exists jq; then
            existing=$(jq -r '.system.geo.ipdat_path // empty' "$B4_CONFIG_FILE" 2>/dev/null) || true
            if [ -n "$existing" ] && [ "$existing" != "null" ]; then
                if is_abs_path "$existing"; then
                    save_dir=$(dirname "$existing")
                    log_info "Found existing geoip path: $save_dir"
                else
                    log_warn "Ignoring non-absolute geoip path in config: $existing"
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
        log_err "GeoIP save directory must be an absolute path (got: ${save_dir:-empty})"
        return 1
    fi

    ensure_dir "$save_dir" "GeoIP directory" || return 1

    # Download
    log_info "Downloading geoip.dat..."
    if ! fetch_file "${base_url}/geoip.dat" "${save_dir}/geoip.dat"; then
        log_err "Failed to download geoip.dat"
        return 1
    fi
    [ ! -s "${save_dir}/geoip.dat" ] && log_err "geoip.dat is empty" && return 1

    log_ok "geoip.dat downloaded to ${save_dir}"

    # Update config (uses shared helper from geosite.sh)
    _geo_update_config "ipdat_path" "${save_dir}/geoip.dat" "ipdat_url" "${base_url}/geoip.dat"
}

feature_geoip_remove() {
    _geo_remove_file "ipdat_path" "geoip.dat"
}

register_feature "geoip"

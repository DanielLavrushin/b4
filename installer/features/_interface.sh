#!/bin/sh
# Feature registration and dispatch system
#
# Each feature file must define these functions (prefixed with feature_<id>_):
#   name             — Human-readable name
#   description      — Short description
#   default_enabled  — "yes" or "no"
#   run              — Execute the feature (install/configure)
#   remove           — Undo/clean up the feature
#
# Then register with: register_feature "<id>"

REGISTERED_FEATURES=""
ENABLED_FEATURES=""
FAILED_FEATURES=""

register_feature() {
    id="$1"
    REGISTERED_FEATURES="${REGISTERED_FEATURES} ${id}"
}

# Dispatch to a specific feature
feature_dispatch() {
    fid="$1"
    func="$2"
    shift 2
    fn="feature_${fid}_${func}"
    if type "$fn" >/dev/null 2>&1; then
        "$fn" "$@"
    else
        log_warn "Feature '${fid}' does not implement '${func}'"
        return 1
    fi
}

features_prepare() {
    for f in $ENABLED_FEATURES; do
        type "feature_${f}_prepare" >/dev/null 2>&1 || continue
        fname=$(feature_dispatch "$f" name)
        log_header "Feature: ${fname}"
        feature_dispatch "$f" prepare || true
    done
}

# Run all enabled features
features_run() {
    for f in $ENABLED_FEATURES; do
        fname=$(feature_dispatch "$f" name)
        _fr_name="$fname"
        log_header "Feature: ${_fr_name}"
        if ! feature_dispatch "$f" run; then
            log_warn "Feature '${_fr_name}' had issues"
            FAILED_FEATURES="${FAILED_FEATURES:+${FAILED_FEATURES}, }${_fr_name}"
        fi
    done
}

# Remove all registered features
features_remove() {
    # Collect geodata files first for a combined prompt
    _geo_files_to_remove=""
    _geo_files_display=""
    for f in $REGISTERED_FEATURES; do
        case "$f" in
        geoip|geosite)
            _gpath=$(_geo_find_file_path "$f")
            if [ -n "$_gpath" ]; then
                _geo_files_to_remove="${_geo_files_to_remove} ${_gpath}"
                _geo_files_display="${_geo_files_display}\n    ${_gpath}"
            fi
            ;;
        *)
            feature_dispatch "$f" remove || true
            ;;
        esac
    done

    # Ask once for all geodata files
    if [ -n "$_geo_files_to_remove" ]; then
        log_info "Found geodata files:${_geo_files_display}"
        if [ "$QUIET_MODE" -eq 1 ] || confirm "Remove geodata files?" "y"; then
            for _gf in $_geo_files_to_remove; do
                rm -f "$_gf" && log_info "Removed: $_gf"
            done
        else
            log_info "Keeping geodata files"
        fi
    fi
}

#!/bin/sh
# Feature: Web UI authentication
# Sets up username/password protection for the B4 web interface

feature_auth_name() {
    echo "Web UI authentication"
}

feature_auth_description() {
    echo "Protect the web interface with a login/password"
}

feature_auth_default_enabled() {
    echo "no"
}

feature_auth_run() {
    log_info "Set up credentials for the B4 web interface"
    echo ""

    read_input "  Username: " ""
    _auth_user="$_INPUT"

    if [ -z "$_auth_user" ]; then
        log_info "No username provided, skipping authentication setup"
        return 0
    fi

    # Read password with confirmation
    while true; do
        printf "  Password: " >&2
        stty -echo 2>/dev/null || true
        read -r _auth_pass
        stty echo 2>/dev/null || true
        echo "" >&2

        if [ -z "$_auth_pass" ]; then
            log_warn "Password cannot be empty"
            continue
        fi

        printf "  Confirm password: " >&2
        stty -echo 2>/dev/null || true
        read -r _auth_pass2
        stty echo 2>/dev/null || true
        echo "" >&2

        if [ "$_auth_pass" != "$_auth_pass2" ]; then
            log_warn "Passwords do not match, try again"
            continue
        fi

        break
    done

    if ! command_exists jq; then
        log_warn "jq not found — please update config manually:"
        log_info "  Set system.web_server.username = $_auth_user"
        log_info "  Set system.web_server.password = <your password>"
        return 0
    fi

    if [ ! -f "$B4_CONFIG_FILE" ]; then
        ensure_dir "$(dirname "$B4_CONFIG_FILE")" "Config directory" || return 1
        (umask 077 && jq -n \
            --arg user "$_auth_user" \
            --arg pass "$_auth_pass" \
            '{ system: { web_server: { username: $user, password: $pass } } }' \
            >"$B4_CONFIG_FILE")
        config_secure_perms
    else
        tmp="${B4_CONFIG_FILE}.tmp"
        if (umask 077 && jq --arg user "$_auth_user" --arg pass "$_auth_pass" \
            '.system.web_server.username = $user | .system.web_server.password = $pass' \
            "$B4_CONFIG_FILE" >"$tmp" 2>/dev/null); then
            mv "$tmp" "$B4_CONFIG_FILE"
            config_secure_perms
        else
            rm -f "$tmp"
            log_warn "Failed to update config"
            return 1
        fi
    fi

    log_ok "Web UI authentication configured for user '${_auth_user}'"
}

feature_auth_remove() {
    return 0
}

register_feature "auth"

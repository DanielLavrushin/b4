#!/bin/sh
# Logging functions

QUIET_MODE=0
# B4_UPDATE_LOG — when set (web UI update), every log line is also appended
# here, timestamped and color-free, regardless of QUIET_MODE. Gives a full
# trace of the update session even though the terminal output is suppressed.
B4_UPDATE_LOG="${B4_UPDATE_LOG:-}"

# Append a timestamped, color-free line to the update log if one is configured.
# Usage: _log_emit <TAG> <message>
_log_emit() {
    [ -z "$B4_UPDATE_LOG" ] && return
    printf '%s [%-4s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null)" "$1" "$2" \
        >>"$B4_UPDATE_LOG" 2>/dev/null || true
}

log_info() {
    _log_emit "INFO" "$1"
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${BLUE}[INFO]${NC} %s\n" "$1" >&2
}

log_ok() {
    _log_emit "OK" "$1"
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${GREEN}[ OK ]${NC} %s\n" "$1" >&2
}

log_warn() {
    _log_emit "WARN" "$1"
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${YELLOW}[WARN]${NC} %s\n" "$1" >&2
}

log_err() {
    _log_emit "ERR" "$1"
    printf "${RED}[ERR ]${NC} %s\n" "$1" >&2
}

log_header() {
    _log_emit "----" "$1"
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "\n${MAGENTA}${BOLD}%s${NC}\n" "$1" >&2
}

log_detail() {
    _log_emit "INFO" "$1: $2"
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "  ${CYAN}%-22s${NC}: %b\n" "$1" "$2" >&2
}

# Print a separator line
log_sep() {
    [ "$QUIET_MODE" -eq 1 ] && return
    printf "${DIM}%s${NC}\n" "─────────────────────────────────────────" >&2
}

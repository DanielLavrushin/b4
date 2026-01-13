#!/bin/sh
# Configuration
REPO_OWNER="DanielLavrushin"
REPO_NAME="b4"
# These will be set dynamically by set_system_paths()
INSTALL_DIR=""
CONFIG_DIR=""
SERVICE_DIR=""
SERVICE_NAME=""
SYSTEM_TYPE=""
BINARY_NAME="b4"
CONFIG_FILE="" # Will be set after CONFIG_DIR is determined
TEMP_DIR="/tmp/b4_install_$$"
QUIET_MODE="0"
GEOSITE_SRC=""
GEOSITE_DST=""
# Proxy configuration for GitHub fallback
PROXY_BASE_URL="https://proxy.lavrush.in/github"

# geodat sources (pipe-delimited: number|name|url)
GEODAT_SOURCES="1|Loyalsoldier source|https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download
2|RUNET Freedom source [recommended]|https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release
3|Nidelon source|https://github.com/Nidelon/ru-block-v2ray-rules/releases/latest/download
4|DustinWin source|https://github.com/DustinWin/ruleset_geodata/releases/download/mihomo
5|Chocolate4U source|https://raw.githubusercontent.com/Chocolate4U/Iran-v2ray-rules/release"

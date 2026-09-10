#!/bin/sh
# B4 Installer — Universal Linux installer with wizard interface
# Supports desktop Linux, OpenWRT, MerlinWRT, Keenetic, Mikrotik, Docker, and more
#
# AUTO-GENERATED — Do not edit directly
# Edit files in installer/ and run: make build-installer
#

set -e

# Ensure sane PATH (Entware paths first for wget-ssl/curl from /opt/bin)
export PATH="/opt/bin:/opt/sbin:$HOME/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin:$PATH"

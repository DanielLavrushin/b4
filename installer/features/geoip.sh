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

feature_geoip_prepare() {
    _geo_prepare geoip
}

feature_geoip_run() {
    _geo_commit geoip
}

feature_geoip_remove() {
    _geo_remove_file "ipdat_path" "geoip.dat"
}

register_feature "geoip"

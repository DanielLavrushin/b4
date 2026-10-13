export const SET_PATH_LABEL_KEYS: Record<string, string> = {
  name: "core.setName",
  "targets.sni_domains": "sets.targets.manualDomains",
  "targets.ip": "sets.targets.manualIps",
  "targets.asns": "sets.targets.asn.activeTitle",
  "targets.source_devices": "core.devices.devicesTitle",
  "targets.geosite_categories": "sets.targets.geositeCategories",
  "targets.geoip_categories": "sets.targets.geoipCategories",
  "fragmentation.strategy_pool": "sets.tcp.splitting.strategyPool",
  "faking.tls_mod": "sets.faking.fakeSni.tlsModTitle",
};

export const SET_VALUE_LABEL_KEYS: Record<string, Record<string, string>> = {
  "fragmentation.strategy_pool": {
    combo: "sets.tcp.splitting.strategyCombo",
    hybrid: "sets.tcp.splitting.strategyHybrid",
    disorder: "sets.tcp.splitting.strategyDisorder",
    extsplit: "sets.tcp.splitting.strategyExtSplit",
    firstbyte: "sets.tcp.splitting.strategyFirstByte",
    tcp: "sets.tcp.splitting.strategyTcp",
    ip: "sets.tcp.splitting.strategyIp",
    tls: "sets.tcp.splitting.strategyTls",
    oob: "sets.tcp.splitting.strategyOob",
    none: "sets.tcp.splitting.strategyNone",
  },
  "faking.tls_mod": {
    rnd: "sets.faking.fakeSni.randomizeTlsRandom",
    dupsid: "sets.faking.fakeSni.dupSessionId",
  },
};

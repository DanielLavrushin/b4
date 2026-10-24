import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";

// Dialog label keys derived from config paths by convention: longest-prefix
// namespace match, remaining segments camelCased, trailing _sec stripped
// ("system.dns.tcp_port" -> "settings.Dns.tcpPort"). Anything the convention
// cannot spell lives in LABEL_EXCEPTIONS below.
const LABEL_NS_BY_PREFIX: Record<string, string> = {
  dns: "sets.dns",
  routing: "sets.routing",
  targets: "sets.targets",
  "fragmentation.combo": "sets.tcp.splitting.combo",
  "fragmentation.disorder": "sets.tcp.splitting.disorder",
  fragmentation: "sets.tcp.splitting",
  "faking.sni_mutation": "sets.faking.mutation",
  faking: "sets.faking.fakeSni",
  "tcp.desync": "sets.faking.desync",
  "tcp.incoming": "sets.faking.incoming",
  "tcp.win": "sets.faking.window",
  tcp: "sets.tcp.general",
  mss_clamp: "sets.tcp.general",
  udp: "sets.udp",
  escalate: "sets.escalation",
  "queue.devices": "settings.Devices",
  "system.ai": "settings.Ai",
  "system.dns": "settings.Dns",
  "system.geo": "settings.Geo",
  "system.hub": "settings.Hub",
  "system.ip_health": "settings.IPHealth",
  "system.logging": "settings.Logging",
  "system.tables": "settings.Feature",
  "system.socks5": "settings.Socks5",
  "system.mtproto": "settings.MTProto",
  "system.web_server.mcp": "settings.Mcp",
  "system.web_server": "settings.WebServer",
  "system.checker.watchdog": "settings.Watchdog",
  "system.checker": "settings.Checker",
};

// Labels the convention cannot derive: arbitrary names ("modeLabel"),
// cross-namespace keys ("core.interfaces") and inverted flags ("tcpEnable").
// Set entries in editor tab order, settings entries alphabetically.
const LABEL_EXCEPTIONS: Record<string, string> = {
  name: "core.setName",
  enabled: "core.enabled",
  "targets.sni_domains": "sets.targets.manualDomains",
  "targets.ip": "sets.targets.manualIps",
  "targets.asns": "sets.targets.asn.activeTitle",
  "targets.source_devices": "core.devices.devicesTitle",
  "faking.tls_mod": "sets.faking.fakeSni.tlsModTitle",
  "queue.devices.enabled": "settings.Devices.enable",
  "queue.devices.wisb": "settings.Devices.invertSelection",
  "queue.interfaces": "core.interfaces",
  "queue.ipv4": "settings.Feature.enableIPv4",
  "queue.ipv6": "settings.Feature.enableIPv6",
  "queue.mark": "settings.Queue.packetMark",
  "queue.mode": "settings.Feature.engineModeLabel",
  "queue.mss_clamp.enabled": "settings.MSSClamping.enable",
  "queue.mss_clamp.size": "settings.MSSClamping.mssSize",
  "queue.start_num": "settings.Queue.queueStart",
  "queue.tcp_conn_bytes_limit": "settings.Queue.tcpLimit",
  "queue.threads": "settings.Queue.workerThreads",
  "queue.tun.address": "settings.Feature.tunAddress",
  "queue.tun.device_name": "settings.Feature.tunDeviceName",
  "queue.tun.out_gateway": "settings.Feature.tunOutGateway",
  "queue.tun.out_interface": "settings.Feature.tunOutInterface",
  "queue.udp_conn_bytes_limit": "settings.Queue.udpLimit",
  "system.ai.enabled": "settings.Ai.title",
  "system.api.ipinfo_token": "settings.Api.token",
  "system.checker.config_propagate_ms": "settings.Checker.configPropagation",
  "system.checker.dns_server": "settings.Checker.trustedDns",
  "system.checker.watchdog.domains": "settings.Watchdog.domainsConfig",
  "system.dns.keep_ipv6_answers": "settings.Dns.ipv4Fallback",
  "system.dns.tcp_disabled": "settings.Dns.tcpEnable",
  "system.logging.directory": "settings.Logging.logDirectory",
  "system.logging.instaflush": "settings.Logging.instantFlush",
  "system.logging.level": "settings.Logging.logLevel",
  "system.memory_limit": "settings.Service.memoryLimit",
  "system.mtproto.bind_address": "settings.Listener.bindAddress",
  "system.mtproto.bridge.enabled": "settings.TelegramBridge.title",
  "system.mtproto.cfproxy_enabled": "settings.MTProto.cfProxyEnabled",
  "system.mtproto.cfproxy_url": "settings.MTProto.cfProxyURL",
  "system.mtproto.cfworker_domain": "settings.MTProto.cfWorkerDomain",
  "system.mtproto.cfworker_dpi": "settings.MTProto.cfWorkerDpi",
  "system.mtproto.dc_fallback_url": "settings.MTProto.dcFallbackURL",
  "system.mtproto.enabled": "settings.MTProto.title",
  "system.mtproto.expose": "settings.Listener.expose",
  "system.mtproto.fake_sni": "settings.MTProto.fakeSNI",
  "system.mtproto.port": "settings.Listener.port",
  "system.mtproto.secrets": "settings.MTProto.secretsTitle",
  "system.mtproto.web_proxy.enabled": "settings.MTProto.webProxyTitle",
  "system.mtproto.web_proxy.expose": "settings.Listener.expose",
  "system.socks5.bind_address": "settings.Listener.bindAddress",
  "system.socks5.enabled": "settings.Socks5.enable",
  "system.socks5.expose": "settings.Listener.expose",
  "system.socks5.port": "settings.Listener.port",
  "system.tables.dscp.enabled": "settings.Feature.dscp",
  "system.tables.dscp.interfaces": "core.interfaces",
  "system.tables.engine": "settings.Feature.firewallEngine",
  "system.tables.masquerade.enabled": "settings.Feature.natMasqueradeEnable",
  "system.tables.masquerade.interfaces": "core.interfaces",
  "system.tables.monitor_interval": "settings.Feature.firewallMonitorInterval",
  "system.tables.skip_setup": "settings.Feature.skipIptables",
  "system.timezone": "settings.Service.timezone",
  "system.update.mirrors": "settings.Service.updateMirrors",
  "system.web_server.bind_address": "settings.Listener.bindAddress",
  "system.web_server.expose": "settings.Listener.expose",
  "system.web_server.language": "core.language",
  "system.web_server.mcp.allow_active_probes": "settings.Mcp.allowProbes",
  "system.web_server.mcp.enabled": "settings.Mcp.title",
  "system.web_server.port": "settings.Listener.port",
};

export function labelKeyForPath(path: string): string | undefined {
  const hit = LABEL_EXCEPTIONS[path];
  if (hit !== undefined) return hit;
  const segs = path.split(".");
  const maxN = Math.min(3, segs.length - 1);
  for (let n = maxN; n >= 1; n--) {
    const ns = LABEL_NS_BY_PREFIX[segs.slice(0, n).join(".")];
    if (ns === undefined) continue;
    const raw = segs.slice(n).join("_").replace(/_(sec)$/, "");
    const field = raw.replace(/_([a-z0-9])/g, (_m: string, c: string) => c.toUpperCase());
    const key = `${ns}.${field}`;
    // Missing keys (dynamic paths like dns.pins.<domain>) must fall back to
    // the caller instead of rendering the key itself.
    return i18n.exists(key) ? key : undefined;
  }
  return undefined;
}


export function useFieldLabel(path: string | undefined, label: string | undefined): string | undefined {
  const { t } = useTranslation();
  if (label !== undefined) return label;
  if (path === undefined) return undefined;
  const key = labelKeyForPath(path);
  return key === undefined ? undefined : t(key);
}
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

export const humanizeSegment = (segment: string): string =>
  segment.replaceAll("_", " ");

export const capitalize = (text: string): string =>
  text.length > 0 ? text[0].toUpperCase() + text.slice(1) : text;

// Object maps (dns.pins.<domain>) have no label of their own; use the nearest
// registered ancestor and keep the dynamic remainder visible.
export const labelForPath = (
  path: string,
  labels: ReadonlyMap<string, ReactNode>,
): ReactNode => {
  const segs = path.split(".");
  for (let n = segs.length; n > 0; n--) {
    const label = labels.get(segs.slice(0, n).join("."));
    if (label === undefined) continue;
    const rest = segs.slice(n).join(".");
    if (!rest) return label;
    return typeof label === "string" ? `${label}: ${rest}` : rest;
  }
  return capitalize(humanizeSegment(segs[segs.length - 1]));
};

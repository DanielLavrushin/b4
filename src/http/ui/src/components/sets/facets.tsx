import {
  BlockIcon,
  DnsIcon,
  DomainIcon,
  EscalateIcon,
  FakingIcon,
  FragIcon,
  RoutingIcon,
} from "@b4.icons";
import { colors, facets } from "@design";
import {
  B4SetConfig,
  FakingPayloadType,
  RoutingMode,
} from "@models/config";
import { SetStats } from "./Manager";

export type FacetKey =
  | "target"
  | "split"
  | "fake"
  | "route"
  | "dns"
  | "escalate";

export const FACET_ORDER: FacetKey[] = [
  "target",
  "split",
  "fake",
  "route",
  "dns",
  "escalate",
];

export interface EditorSection {
  tab: string;
  sub?: string;
}

export const FACET_SECTIONS: Record<FacetKey, EditorSection> = {
  target: { tab: "targets", sub: "domains" },
  split: { tab: "tcp", sub: "splitting" },
  fake: { tab: "tcp", sub: "faking" },
  route: { tab: "routing", sub: "traffic" },
  dns: { tab: "routing", sub: "dns" },
  escalate: { tab: "escalation" },
};

export const FACET_COLORS: Record<FacetKey, string> = {
  target: facets.target,
  split: facets.split,
  fake: facets.fake,
  route: facets.route,
  dns: facets.dns,
  escalate: facets.escalate,
};

export interface FacetRow {
  label: string;
  value: string;
  muted?: string;
}

export interface SetFacet {
  key: FacetKey;
  label: string;
  color: string;
  icon: React.ReactElement;
  active: boolean;
  rows: FacetRow[];
  section: EditorSection;
}

export const STRATEGY_LABELS: Record<string, string> = {
  combo: "COMBO",
  hybrid: "HYBRID",
  disorder: "DISORDER",
  overlap: "OVERLAP",
  extsplit: "EXT SPLIT",
  firstbyte: "1ST BYTE",
  tcp: "TCP FRAG",
  ip: "IP FRAG",
  tls: "TLS REC",
  oob: "OOB",
  none: "NONE",
};

const PAYLOAD_LABELS: Record<number, string> = {
  [FakingPayloadType.RANDOM]: "random",
  [FakingPayloadType.CUSTOM]: "custom",
  [FakingPayloadType.DEFAULT]: "default",
  [FakingPayloadType.DEFAULT2]: "default2",
  [FakingPayloadType.CAPTURE]: "capture",
  [FakingPayloadType.ZERO]: "zero",
  [FakingPayloadType.INVERTED]: "inverted",
  [FakingPayloadType.DOMAIN]: "domain",
  [FakingPayloadType.STUN]: "stun",
};

type TFn = (key: string) => string;

const F = (key: string) => `sets.card.f.${key}`;

const range = (min: number, max: number) =>
  max > min ? `${min}–${max}` : String(min);

const onOff = (t: TFn, value: boolean) => (value ? t(F("on")) : t(F("off")));

export const resolveRoutingMode = (
  mode: RoutingMode | undefined,
): RoutingMode => {
  if (mode === "proxy") return "proxy";
  if (mode === "mtproto-ws") return "mtproto-ws";
  if (mode === "block") return "block";
  return "interface";
};

const targetRows = (
  set: B4SetConfig,
  stats: SetStats | undefined,
  t: TFn,
): FacetRow[] => {
  const rows: FacetRow[] = [];
  const { targets } = set;

  if (targets.geosite_categories.length > 0) {
    rows.push({
      label: t(F("geosite")),
      value: targets.geosite_categories.join(", "),
    });
  }
  if (targets.geoip_categories.length > 0) {
    rows.push({
      label: t(F("geoip")),
      value: targets.geoip_categories.join(", "),
    });
  }

  const domains = stats?.total_domains ?? targets.sni_domains.length;
  const ips = stats?.total_ips ?? targets.ip.length;
  const mixed = (manual: number, total: number) =>
    manual > 0 && manual < total
      ? `${manual} ${t("sets.card.manual")}`
      : undefined;

  rows.push({
    label: t("core.domains"),
    value: domains.toLocaleString(),
    muted: stats ? mixed(stats.manual_domains, domains) : undefined,
  });
  rows.push({
    label: t("core.ips"),
    value: ips.toLocaleString(),
    muted: stats ? mixed(stats.manual_ips, ips) : undefined,
  });

  const devices = targets.source_devices?.length ?? 0;
  if (devices > 0) {
    rows.push({
      label: t(F("devices")),
      value: String(devices),
      muted: targets.source_devices_exclude
        ? t(F("excluded"))
        : t(F("included")),
    });
  }
  if (targets.tls) {
    rows.push({ label: t(F("tls")), value: targets.tls });
  }
  if (targets.ip_version) {
    rows.push({ label: t(F("ipVersion")), value: targets.ip_version });
  }
  if (set.tcp?.dport_filter) {
    rows.push({ label: t(F("tcpPorts")), value: set.tcp.dport_filter });
  }
  if (set.udp?.dport_filter) {
    rows.push({ label: t(F("udpPorts")), value: set.udp.dport_filter });
  }
  return rows;
};

const splitRows = (set: B4SetConfig, t: TFn): FacetRow[] => {
  const frag = set.fragmentation;
  const rows: FacetRow[] = [
    { label: t(F("strategy")), value: STRATEGY_LABELS[frag.strategy] ?? frag.strategy },
    {
      label: t(F("order")),
      value: frag.reverse_order ? t(F("reversed")) : t(F("inOrder")),
    },
  ];

  if (frag.strategy === "combo") {
    rows.push({ label: t(F("shuffle")), value: frag.combo.shuffle_mode });
    rows.push({
      label: t(F("delay")),
      value: `${range(frag.combo.first_delay_ms, frag.combo.first_delay_ms_max)} ms`,
    });
  } else if (frag.strategy === "disorder") {
    rows.push({ label: t(F("shuffle")), value: frag.disorder.shuffle_mode });
    rows.push({
      label: t(F("jitter")),
      value: `${range(frag.disorder.min_jitter_us, frag.disorder.max_jitter_us)} µs`,
    });
  } else if (frag.strategy === "oob") {
    rows.push({
      label: t(F("oobPos")),
      value: range(frag.oob_position, frag.oob_position_max),
    });
    rows.push({
      label: t(F("oobChar")),
      value: `0x${frag.oob_char.toString(16)}`,
    });
  } else if (frag.strategy === "tls") {
    rows.push({
      label: t(F("recPos")),
      value: range(frag.tlsrec_pos, frag.tlsrec_pos_max),
    });
  } else {
    rows.push({
      label: t(F("sniPos")),
      value: range(frag.sni_position, frag.sni_position_max),
    });
  }

  if (frag.seq_overlap_length > 0) {
    rows.push({
      label: t(F("overlapLen")),
      value: String(frag.seq_overlap_length),
    });
  }
  if (set.mss_clamp?.enabled) {
    rows.push({ label: t(F("mss")), value: String(set.mss_clamp.size) });
  }
  return rows;
};

const fakeRows = (set: B4SetConfig, t: TFn): FacetRow[] => {
  const fake = set.faking;
  const rows: FacetRow[] = [
    {
      label: t(F("payload")),
      value: PAYLOAD_LABELS[fake.sni_type] ?? String(fake.sni_type),
      muted: fake.payload_domain || fake.payload_file || undefined,
    },
    { label: t(F("strategy")), value: fake.strategy },
  ];
  if (fake.apply_ttl) {
    rows.push({ label: t(F("ttl")), value: String(fake.ttl) });
  }
  if (fake.sni_seq_length > 0) {
    rows.push({ label: t(F("seqLen")), value: String(fake.sni_seq_length) });
  }
  if (fake.tcp_md5 || fake.md5_on_fake) {
    rows.push({
      label: t(F("md5")),
      value: fake.md5_on_fake ? t(F("onFakeOnly")) : t(F("on")),
    });
  }
  if (fake.sni_mutation?.mode && fake.sni_mutation.mode !== "off") {
    rows.push({ label: t(F("mutation")), value: fake.sni_mutation.mode });
  }
  if (fake.tls_mod?.length) {
    rows.push({ label: t(F("tlsMod")), value: fake.tls_mod.join(", ") });
  }
  return rows;
};

const routeRows = (set: B4SetConfig, t: TFn): FacetRow[] => {
  const routing = set.routing;
  if (routesViaPins(set)) {
    return [
      { label: t(F("mode")), value: t(F("dnsPin")) },
      { label: t(F("pins")), value: dnsPinnedAddresses(set).join(", ") },
      { label: t(F("egress")), value: t(F("defaultRoute")).toLowerCase() },
    ];
  }
  const mode = resolveRoutingMode(routing.mode);
  const rows: FacetRow[] = [{ label: t(F("mode")), value: mode }];

  if (mode === "block") {
    rows.push({
      label: t(F("action")),
      value: routing.block_action || "reject",
    });
    return rows;
  }
  if (mode === "proxy") {
    const up = routing.upstream;
    rows.push({
      label: t(F("host")),
      value: up?.host ? `${up.host}:${up.port}` : "—",
    });
    rows.push({ label: t(F("udpRelay")), value: onOff(t, !!up?.udp) });
    rows.push({ label: t(F("failOpen")), value: onOff(t, !!up?.fail_open) });
    if (up?.use_domain) {
      rows.push({ label: t(F("useDomain")), value: onOff(t, true) });
    }
    return rows;
  }
  if (mode === "mtproto-ws") {
    rows.push({ label: t(F("transport")), value: "websocket" });
    return rows;
  }

  rows.push({
    label: t(F("egress")),
    value: routing.egress_interface || "—",
  });
  if (routing.egress_ip) {
    rows.push({ label: t(F("egressIp")), value: routing.egress_ip });
  }
  if (routing.table) {
    rows.push({
      label: t(F("table")),
      value: String(routing.table),
      muted: routing.fwmark ? `fwmark 0x${routing.fwmark.toString(16)}` : undefined,
    });
  }
  return rows;
};

export const dnsPinnedDomains = (set: B4SetConfig): string[] =>
  Object.keys(set.dns?.pins ?? {});

export const dnsPinnedAddresses = (set: B4SetConfig): string[] => [
  ...new Set(Object.values(set.dns?.pins ?? {}).flat()),
];

export const routesViaPins = (set: B4SetConfig) =>
  !set.routing?.enabled && dnsPinnedDomains(set).length > 0;

export const hasDnsFacet = (set: B4SetConfig) =>
  !!set.dns?.enabled || dnsPinnedDomains(set).length > 0;

const dnsRows = (set: B4SetConfig, t: TFn): FacetRow[] => {
  const dns = set.dns;
  const isDoh = !!dns.doh_url;
  const rows: FacetRow[] = [];

  if (dns.enabled) {
    rows.push({ label: t(F("mode")), value: isDoh ? "DoH" : t(F("redirect")) });
    rows.push({
      label: t(F("dnsTarget")),
      value: isDoh ? dns.doh_url : dns.target_dns || "—",
    });
    rows.push({ label: t(F("fragment")), value: onOff(t, dns.fragment_query) });
  }

  const pinned = dnsPinnedDomains(set);
  if (pinned.length > 0) {
    if (!dns.enabled) {
      rows.push({ label: t(F("mode")), value: t(F("pinsOnly")) });
    }
    rows.push({ label: t(F("pins")), value: String(pinned.length) });
    rows.push({ label: t("core.domains"), value: pinned.join(", ") });
  }
  return rows;
};

const escalateRows = (
  set: B4SetConfig,
  t: TFn,
  targetName?: string,
): FacetRow[] => {
  const esc = set.escalate;
  if (!esc) return [];
  const rows: FacetRow[] = [
    { label: t(F("to")), value: targetName || esc.to || "—" },
  ];
  if (esc.rst_threshold) {
    rows.push({
      label: t(F("onRst")),
      value: `${esc.rst_threshold}`,
      muted: esc.rst_window_sec ? `/ ${esc.rst_window_sec}s` : undefined,
    });
  }
  if (esc.stall_threshold) {
    rows.push({
      label: t(F("onStall")),
      value: `${esc.stall_threshold}`,
      muted: esc.stall_timeout_ms ? `/ ${esc.stall_timeout_ms}ms` : undefined,
    });
  }
  if (esc.dns_threshold) {
    rows.push({ label: t(F("onDns")), value: String(esc.dns_threshold) });
  }
  if (esc.ttl_sec) {
    rows.push({ label: t(F("ttlSec")), value: `${esc.ttl_sec}s` });
  }
  return rows;
};

export const hasTargets = (set: B4SetConfig) =>
  set.targets.geosite_categories.length > 0 ||
  set.targets.geoip_categories.length > 0 ||
  set.targets.sni_domains.length > 0 ||
  set.targets.ip.length > 0;

export const buildSetFacets = (
  set: B4SetConfig,
  stats: SetStats | undefined,
  t: TFn,
  escalatesToName?: string,
): SetFacet[] => {
  const routeMode = resolveRoutingMode(set.routing?.mode);
  const isBlock = !!set.routing?.enabled && routeMode === "block";
  const pinnedRoute = routesViaPins(set);

  return [
    {
      key: "target",
      label: t(F("target")),
      color: FACET_COLORS.target,
      icon: <DomainIcon />,
      active: hasTargets(set),
      rows: targetRows(set, stats, t),
      section: FACET_SECTIONS.target,
    },
    {
      key: "split",
      label: t(F("split")),
      color: FACET_COLORS.split,
      icon: <FragIcon />,
      active: set.fragmentation.strategy !== "none",
      rows: splitRows(set, t),
      section: FACET_SECTIONS.split,
    },
    {
      key: "fake",
      label: t(F("fake")),
      color: FACET_COLORS.fake,
      icon: <FakingIcon />,
      active: set.faking.sni,
      rows: fakeRows(set, t),
      section: FACET_SECTIONS.fake,
    },
    {
      key: "route",
      label: isBlock ? t(F("block")) : t(F("route")),
      color: isBlock ? facets.block : FACET_COLORS.route,
      icon: isBlock ? <BlockIcon /> : <RoutingIcon />,
      active: !!set.routing?.enabled || pinnedRoute,
      rows: routeRows(set, t),
      section: FACET_SECTIONS.route,
    },
    {
      key: "dns",
      label: t(F("dns")),
      color: FACET_COLORS.dns,
      icon: <DnsIcon />,
      active: hasDnsFacet(set),
      rows: dnsRows(set, t),
      section: FACET_SECTIONS.dns,
    },
    {
      key: "escalate",
      label: t(F("escalate")),
      color: FACET_COLORS.escalate,
      icon: <EscalateIcon />,
      active: !!set.escalate?.to,
      rows: escalateRows(set, t, escalatesToName),
      section: FACET_SECTIONS.escalate,
    },
  ];
};

export const buildTargetSummary = (
  set: B4SetConfig,
  stats: SetStats | undefined,
  t: TFn,
): string => {
  const { targets } = set;
  const named = [
    ...targets.geosite_categories,
    ...targets.geoip_categories,
    ...targets.sni_domains,
    ...targets.ip,
  ];
  if (named.length === 0) return t("sets.card.noTargets");

  const parts: string[] = [];
  parts.push(named.length > 1 ? `${named[0]} +${named.length - 1}` : named[0]);

  const domains = stats?.total_domains ?? targets.sni_domains.length;
  const ips = stats?.total_ips ?? targets.ip.length;
  if (domains > 0) parts.push(`${domains.toLocaleString()} ${t("core.domains")}`);
  if (ips > 0) parts.push(`${ips.toLocaleString()} ${t("core.ips")}`);
  if (targets.tls) parts.push(`TLS ${targets.tls}`);
  if (targets.ip_version) parts.push(targets.ip_version);

  return parts.join(" · ");
};

export interface RouteSummary {
  text: string;
  color: string;
  icon: React.ReactElement;
}

export const buildRouteSummary = (
  set: B4SetConfig,
  t: TFn,
): RouteSummary => {
  const routing = set.routing;
  if (!routing?.enabled) {
    const pinned = routesViaPins(set);
    return {
      text: pinned
        ? `${t(F("defaultRoute"))} · ${t(F("dnsPin"))}`
        : t(F("defaultRoute")),
      color: pinned ? facets.route : colors.text.disabled,
      icon: <RoutingIcon />,
    };
  }

  const mode = resolveRoutingMode(routing.mode);
  if (mode === "block") {
    return {
      text: `${t(F("block")).toLowerCase()} · ${routing.block_action || "reject"}`,
      color: facets.block,
      icon: <BlockIcon />,
    };
  }
  if (mode === "proxy") {
    const up = routing.upstream;
    return {
      text: up?.host ? `socks5 ${up.host}:${up.port}` : "socks5",
      color: facets.route,
      icon: <RoutingIcon />,
    };
  }
  if (mode === "mtproto-ws") {
    return {
      text: "mtproto-ws",
      color: facets.route,
      icon: <RoutingIcon />,
    };
  }
  return {
    text: routing.egress_interface
      ? `iface ${routing.egress_interface}`
      : t(F("defaultRoute")),
    color: facets.route,
    icon: <RoutingIcon />,
  };
};

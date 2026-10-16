import { colors, facets } from "../tokens";
import {
  FacetBlockIcon as BlockIcon,
  FacetDnsIcon as DnsIcon,
  FacetTargetIcon as DomainIcon,
  FacetEscalateIcon as EscalateIcon,
  FacetFakeIcon as FakingIcon,
  FacetSplitIcon as FragIcon,
  FacetRouteIcon as RoutingIcon,
} from "./icons";

export interface FacetStats {
  manual_domains: number;
  manual_ips: number;
  total_domains: number;
  total_ips: number;
  asn_unresolved?: string[];
}

export interface FacetSetConfig {
  tcp?: { dport_filter: string };
  udp?: { dport_filter: string };
  fragmentation: {
    strategy: string;
    reverse_order: boolean;
    sni_position: number;
    sni_position_max: number;
    oob_position: number;
    oob_position_max: number;
    oob_char: number;
    tlsrec_pos: number;
    tlsrec_pos_max: number;
    seq_overlap_length: number;
    combo: {
      shuffle_mode: string;
      first_delay_ms: number;
      first_delay_ms_max: number;
    };
    disorder: {
      shuffle_mode: string;
      min_jitter_us: number;
      max_jitter_us: number;
    };
  };
  faking: {
    sni: boolean;
    sni_type: number;
    strategy: string;
    ttl: number;
    apply_ttl: boolean;
    sni_seq_length: number;
    payload_domain: string;
    payload_file: string;
    tcp_md5: boolean;
    md5_on_fake: boolean;
    sni_mutation?: { mode: string };
    tls_mod?: string[];
  };
  targets: {
    sni_domains: string[];
    ip: string[];
    geosite_categories: string[];
    geoip_categories: string[];
    asns?: string[];
    source_devices?: string[];
    source_devices_exclude?: boolean;
    tls?: string;
    ip_version?: string;
  };
  mss_clamp?: { enabled: boolean; size: number };
  dscp?: { enabled: boolean; value: number };
  routing: {
    enabled: boolean;
    mode: string;
    block_action: string;
    egress_interface: string;
    egress_ip: string;
    egress_gateway: string;
    table: number;
    fwmark: number;
    upstream?: {
      host: string;
      port: number;
      udp: boolean;
      fail_open: boolean;
      use_domain: boolean;
    };
  };
  dns: {
    enabled: boolean;
    target_dns: string;
    doh_url: string;
    fragment_query: boolean;
    pins?: Record<string, string[]>;
  };
  escalate?: {
    to?: string;
    rst_threshold?: number;
    rst_window_sec?: number;
    stall_threshold?: number;
    stall_timeout_ms?: number;
    dns_threshold?: number;
    ttl_sec?: number;
  };
}

export type FacetRoutingMode = "interface" | "proxy" | "mtproto-ws" | "block";

export type FacetTranslate = (
  key: string,
  options?: Record<string, unknown>,
) => string;

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
  0: "random",
  1: "custom",
  2: "default",
  3: "default2",
  4: "capture",
  5: "zero",
  6: "inverted",
  7: "domain",
  8: "stun",
};

const F = (key: string) => `sets.card.f.${key}`;

const range = (min: number, max: number) =>
  max > min ? `${min}-${max}` : String(min);

const onOff = (t: FacetTranslate, value: boolean) =>
  value ? t(F("on")) : t(F("off"));

export const resolveRoutingMode = (
  mode: string | undefined,
): FacetRoutingMode => {
  if (mode === "proxy") return "proxy";
  if (mode === "mtproto-ws") return "mtproto-ws";
  if (mode === "block") return "block";
  return "interface";
};

const targetRows = (
  set: FacetSetConfig,
  stats: FacetStats | undefined,
  t: FacetTranslate,
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
  const asns = targets.asns ?? [];
  if (asns.length > 0) {
    const unresolved = stats?.asn_unresolved?.length ?? 0;
    rows.push({
      label: t(F("asns")),
      value: asns.map((id) => `AS${id}`).join(", "),
      muted:
        unresolved > 0
          ? `${unresolved} ${t(F("asnPending"))}`
          : undefined,
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

const splitRows = (set: FacetSetConfig, t: FacetTranslate): FacetRow[] => {
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

const fakeRows = (set: FacetSetConfig, t: FacetTranslate): FacetRow[] => {
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

const dscpRows = (
  set: FacetSetConfig,
  t: FacetTranslate,
  refusal?: string,
): FacetRow[] =>
  set.dscp?.enabled
    ? [
        {
          label: t(F("dscp")),
          value: String(set.dscp.value),
          muted: refusal ? t(F("dscpOff"), { reason: refusal }) : undefined,
        },
      ]
    : [];

const routeRows = (
  set: FacetSetConfig,
  t: FacetTranslate,
  dscpRefusal?: string,
): FacetRow[] => {
  const routing = set.routing;
  const dscp = dscpRows(set, t, dscpRefusal);
  if (routesViaPins(set)) {
    return [
      { label: t(F("mode")), value: t(F("dnsPin")) },
      { label: t(F("pins")), value: dnsPinnedAddresses(set).join(", ") },
      { label: t(F("egress")), value: t(F("defaultRoute")).toLowerCase() },
      ...dscp,
    ];
  }
  if (dscp.length > 0 && !routing?.enabled) {
    return [
      { label: t(F("egress")), value: t(F("defaultRoute")).toLowerCase() },
      ...dscp,
    ];
  }
  const mode = resolveRoutingMode(routing.mode);
  const rows: FacetRow[] = [
    {
      label: t(F("mode")),
      value: mode === "mtproto-ws" ? t(F("telegramBridge")) : mode,
    },
  ];

  if (mode === "block") {
    rows.push({
      label: t(F("action")),
      value: routing.block_action || "reject",
    });
    return [...rows, ...dscp];
  }
  if (mode === "proxy") {
    const up = routing.upstream;
    rows.push({
      label: t(F("host")),
      value: up?.host ? `${up.host}:${up.port}` : "-",
    });
    rows.push({ label: t(F("udpRelay")), value: onOff(t, !!up?.udp) });
    rows.push({ label: t(F("failOpen")), value: onOff(t, !!up?.fail_open) });
    if (up?.use_domain) {
      rows.push({ label: t(F("useDomain")), value: onOff(t, true) });
    }
    return [...rows, ...dscp];
  }
  if (mode === "mtproto-ws") {
    rows.push({ label: t(F("transport")), value: "websocket" });
    return [...rows, ...dscp];
  }

  rows.push({
    label: t(F("egress")),
    value: routing.egress_interface || "-",
  });
  if (routing.egress_ip) {
    rows.push({ label: t(F("egressIp")), value: routing.egress_ip });
  }
  if (routing.egress_gateway) {
    rows.push({ label: t(F("egressGateway")), value: routing.egress_gateway });
  }
  if (routing.table) {
    rows.push({
      label: t(F("table")),
      value: String(routing.table),
      muted: routing.fwmark ? `fwmark 0x${routing.fwmark.toString(16)}` : undefined,
    });
  }
  return [...rows, ...dscp];
};

export const dnsPinnedDomains = (set: FacetSetConfig): string[] =>
  Object.keys(set.dns?.pins ?? {});

export const dnsPinnedAddresses = (set: FacetSetConfig): string[] => [
  ...new Set(Object.values(set.dns?.pins ?? {}).flat()),
];

export const routesViaPins = (set: FacetSetConfig) =>
  !set.routing?.enabled && dnsPinnedDomains(set).length > 0;

export const hasDnsFacet = (set: FacetSetConfig) =>
  !!set.dns?.enabled || dnsPinnedDomains(set).length > 0;

const dnsRows = (
  set: FacetSetConfig,
  t: FacetTranslate,
  hideEmptyServer: boolean,
): FacetRow[] => {
  const dns = set.dns;
  const isDoh = !!dns.doh_url;
  const rows: FacetRow[] = [];

  if (dns.enabled) {
    rows.push({ label: t(F("mode")), value: isDoh ? "DoH" : t(F("redirect")) });
    if (isDoh || dns.target_dns || !hideEmptyServer) {
      rows.push({
        label: t(F("dnsTarget")),
        value: isDoh ? dns.doh_url : dns.target_dns || "-",
      });
    }
    rows.push({ label: t(F("fragment")), value: onOff(t, dns.fragment_query) });
  }

  const pinned = dnsPinnedDomains(set);
  if (pinned.length > 0) {
    if (!dns.enabled) {
      rows.push({ label: t(F("mode")), value: t(F("pinsOnly")) });
    }
    rows.push({ label: t(F("pins")), value: String(pinned.length) });
    rows.push({ label: t(F("domains")), value: pinned.join(", ") });
  }
  return rows;
};

const escalateRows = (
  set: FacetSetConfig,
  t: FacetTranslate,
  targetName?: string,
): FacetRow[] => {
  const esc = set.escalate;
  if (!esc) return [];
  const rows: FacetRow[] = [
    { label: t(F("to")), value: targetName || esc.to || "-" },
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

export const hasTargets = (set: FacetSetConfig) =>
  set.targets.geosite_categories.length > 0 ||
  set.targets.geoip_categories.length > 0 ||
  (set.targets.asns?.length ?? 0) > 0 ||
  set.targets.sni_domains.length > 0 ||
  set.targets.ip.length > 0;

export const buildSetFacets = (
  set: FacetSetConfig,
  stats: FacetStats | undefined,
  t: FacetTranslate,
  escalatesToName?: string,
  options?: { hideEmptyDnsServer?: boolean; dscpRefusal?: string },
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
      active: !!set.routing?.enabled || pinnedRoute || !!set.dscp?.enabled,
      rows: routeRows(set, t, options?.dscpRefusal),
      section: FACET_SECTIONS.route,
    },
    {
      key: "dns",
      label: t(F("dns")),
      color: FACET_COLORS.dns,
      icon: <DnsIcon />,
      active: hasDnsFacet(set),
      rows: dnsRows(set, t, !!options?.hideEmptyDnsServer),
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
  set: FacetSetConfig,
  stats: FacetStats | undefined,
  t: FacetTranslate,
): string => {
  const { targets } = set;
  const named = [
    ...targets.geosite_categories,
    ...targets.geoip_categories,
    ...(targets.asns ?? []).map((id) => `AS${id}`),
    ...targets.sni_domains,
    ...targets.ip,
  ];
  if (named.length === 0) return t("sets.card.noTargets");

  const parts: string[] = [];
  parts.push(named.length > 1 ? `${named[0]} +${named.length - 1}` : named[0]);

  const domains = stats?.total_domains ?? targets.sni_domains.length;
  const ips = stats?.total_ips ?? targets.ip.length;
  if (domains > 0) parts.push(t("sets.card.domainCount", { count: domains }));
  if (ips > 0) parts.push(t("sets.card.ipCount", { count: ips }));
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
  set: FacetSetConfig,
  t: FacetTranslate,
  options?: { dscpRefusal?: string },
): RouteSummary => {
  const routing = set.routing;
  const dscp =
    set.dscp?.enabled && !options?.dscpRefusal
      ? ` · DSCP ${set.dscp.value}`
      : "";
  if (!routing?.enabled) {
    const pinned = routesViaPins(set);
    const base = pinned
      ? `${t(F("defaultRoute"))} · ${t(F("dnsPin"))}`
      : t(F("defaultRoute"));
    return {
      text: `${base}${dscp}`,
      color: pinned || dscp ? facets.route : colors.text.disabled,
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
      text: t(F("telegramBridge")),
      color: facets.route,
      icon: <RoutingIcon />,
    };
  }
  const egress = routing.egress_interface
    ? `iface ${routing.egress_interface}`
    : t(F("defaultRoute"));
  return {
    text: `${egress}${dscp}`,
    color: facets.route,
    icon: <RoutingIcon />,
  };
};

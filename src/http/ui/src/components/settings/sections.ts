import type { SvgIconComponent } from "@mui/icons-material";
import {
  BackupIcon,
  ConnectionIcon,
  ControlIcon,
  DeviceUnknowIcon,
  DnsIcon,
  FilterIcon,
  NetworkIcon,
  WebIcon,
} from "@b4.icons";
import { B4Config } from "@models/config";

export type CoreSectionId =
  | "engine"
  | "devices"
  | "firewall"
  | "dns"
  | "socks5";

export type SystemSectionId = "service" | "web" | "backup";

export interface SectionBlockDef {
  prefixes: string[];
  labelKey: string;
}

export interface DialogSubDef {
  prefixes: string[];
  labelKey: string;
  blocks?: SectionBlockDef[];
}

export interface SettingsSection<Id extends string = string> {
  id: Id;
  labelKey: string;
  Icon: SvgIconComponent;
  prefixes: string[];
  exceptPrefixes?: string[];
  blocks?: SectionBlockDef[];
  pick: (c: B4Config) => unknown[];
  restartPick: (c: B4Config) => unknown[];
}

const engineQueue = (c: B4Config) => ({
  ...c.queue,
  devices: undefined,
  mss_clamp: undefined,
});

const engineRestart = (c: B4Config) => ({
  ...engineQueue(c),
  interfaces: undefined,
  tcp_conn_bytes_limit: undefined,
  udp_conn_bytes_limit: undefined,
});

const deviceGate = (c: B4Config) => {
  const { enabled, wisb, devices } = c.queue.devices;
  const matches = devices
    .filter((d) => d.selected)
    .map((d) =>
      d.is_manual
        ? `ip:${(d.ip ?? "").trim().toLowerCase()}`
        : `mac:${d.mac.trim().toUpperCase()}`,
    )
    .filter((m) => m !== "ip:" && m !== "mac:");
  return [enabled, wisb, [...new Set(matches)].sort()];
};

const nothing = () => [];

export const CORE_SECTIONS: SettingsSection<CoreSectionId>[] = [
  {
    id: "engine",
    labelKey: "settings.coreTabs.engine",
    prefixes: ["queue", "system.ip_health", "system.dns.keep_ipv6_answers"],
    exceptPrefixes: ["queue.devices", "queue.mss_clamp"],
    blocks: [
      { prefixes: ["queue.mode", "queue.interfaces", "queue.tun"], labelKey: "settings.Feature.engineMode" },
      { prefixes: ["queue.ipv4", "queue.ipv6", "system.dns.keep_ipv6_answers"], labelKey: "settings.Feature.protoFeatures" },
      { prefixes: ["system.ip_health"], labelKey: "settings.IPHealth.title" },
      {
        prefixes: ["queue.start_num", "queue.mark", "queue.threads", "queue.tcp_conn_bytes_limit", "queue.udp_conn_bytes_limit"],
        labelKey: "settings.Queue.title",
      },
    ],
    Icon: NetworkIcon,
    pick: (c) => [
      engineQueue(c),
      c.system.ip_health,
      c.system.dns?.keep_ipv6_answers,
    ],
    restartPick: (c) => [engineRestart(c)],
  },
  {
    id: "devices",
    labelKey: "settings.coreTabs.devices",
    prefixes: ["queue.devices"],
    Icon: DeviceUnknowIcon,
    pick: (c) => [c.queue.devices],
    restartPick: deviceGate,
  },
  {
    id: "firewall",
    labelKey: "settings.coreTabs.firewall",
    prefixes: ["system.tables", "queue.mss_clamp"],
    blocks: [
      { prefixes: ["system.tables.masquerade"], labelKey: "settings.Feature.natMasquerade" },
      { prefixes: ["system.tables.dscp"], labelKey: "settings.Feature.dscpSettings" },
      { prefixes: ["queue.mss_clamp"], labelKey: "settings.MSSClamping.title" },
    ],
    Icon: FilterIcon,
    pick: (c) => [c.system.tables, c.queue.mss_clamp],
    restartPick: (c) => [
      { ...c.system.tables, dscp: undefined, masquerade: undefined },
    ],
  },
  {
    id: "dns",
    labelKey: "settings.coreTabs.dns",
    prefixes: ["system.dns"],
    exceptPrefixes: ["system.dns.keep_ipv6_answers"],
    Icon: DnsIcon,
    pick: (c) => [{ ...c.system.dns, keep_ipv6_answers: undefined }],
    restartPick: nothing,
  },
  {
    id: "socks5",
    labelKey: "settings.coreTabs.socks5",
    prefixes: ["system.socks5"],
    Icon: ConnectionIcon,
    pick: (c) => [c.system.socks5],
    restartPick: nothing,
  },
];

export const SYSTEM_SECTIONS: SettingsSection<SystemSectionId>[] = [
  {
    id: "service",
    labelKey: "settings.systemTabs.service",
    prefixes: ["system.logging", "system.timezone", "system.memory_limit", "system.update", "system.web_server.language"],
    Icon: ControlIcon,
    pick: (c) => [
      c.system.logging,
      c.system.timezone,
      c.system.memory_limit,
      c.system.update,
      c.system.web_server.language,
    ],
    restartPick: (c) => [
      c.system.logging.instaflush,
      c.system.logging.syslog,
      c.system.memory_limit,
    ],
  },
  {
    id: "web",
    labelKey: "settings.systemTabs.web",
    prefixes: ["system.web_server"],
    exceptPrefixes: ["system.web_server.language", "system.web_server.mcp"],
    Icon: WebIcon,
    pick: (c) => [
      { ...c.system.web_server, mcp: undefined, language: undefined },
    ],
    restartPick: (c) => [
      c.system.web_server.port,
      c.system.web_server.bind_address,
      c.system.web_server.tls_cert,
      c.system.web_server.tls_key,
    ],
  },
  {
    id: "backup",
    labelKey: "settings.systemTabs.backup",
    prefixes: [],
    Icon: BackupIcon,
    pick: nothing,
    restartPick: nothing,
  },
];

export const sectionIndex = (
  sections: SettingsSection[],
  id: string | undefined,
) => {
  const index = sections.findIndex((s) => s.id === id);
  return Math.max(index, 0);
};

export const SETTINGS_DIALOG_SUB_DEFS: DialogSubDef[] = [
  { prefixes: ["system.api"], labelKey: "settings.Api.ipinfoTitle" },
  { prefixes: ["system.ai"], labelKey: "settings.Ai.title" },
  { prefixes: ["system.web_server.mcp"], labelKey: "settings.Mcp.title" },
  { prefixes: ["system.hub"], labelKey: "settings.Hub.title" },
  { prefixes: ["system.geo"], labelKey: "settings.tabs.geodat" },
  {
    prefixes: ["system.checker"],
    labelKey: "settings.tabs.discovery",
    blocks: [{ prefixes: ["system.checker.watchdog"], labelKey: "settings.Watchdog.title" }],
  },
  {
    prefixes: ["system.mtproto"],
    labelKey: "settings.tabs.mtproto",
    blocks: [
      {
        prefixes: [
          "system.mtproto.enabled",
          "system.mtproto.bind_address",
          "system.mtproto.port",
          "system.mtproto.expose",
          "system.mtproto.fake_sni",
          "system.mtproto.max_connections",
          "system.mtproto.tcp_user_timeout_sec",
          "system.mtproto.idle_timeout_sec",
        ],
        labelKey: "settings.MTProto.title",
      },
      { prefixes: ["system.mtproto.secrets"], labelKey: "settings.MTProto.secretsTitle" },
      {
        prefixes: [
          "system.mtproto.upstream_mode",
          "system.mtproto.dc_relay",
          "system.mtproto.cfworker_domain",
          "system.mtproto.cfworker_dpi",
          "system.mtproto.ws_custom_domain",
          "system.mtproto.ws_endpoint_host",
          "system.mtproto.ws_front_sni",
          "system.mtproto.cfproxy_enabled",
          "system.mtproto.cfproxy_url",
          "system.mtproto.dc_fallback_enabled",
          "system.mtproto.dc_fallback_url",
          "system.mtproto.bridge_wait_sec",
        ],
        labelKey: "settings.MTProto.upstreamTitle",
      },
      { prefixes: ["system.mtproto.bridge"], labelKey: "settings.TelegramBridge.title" },
      { prefixes: ["system.mtproto.web_proxy"], labelKey: "settings.MTProto.webProxyTitle" },
    ],
  },
];

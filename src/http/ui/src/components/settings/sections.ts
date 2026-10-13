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

export interface SettingsSection<Id extends string = string> {
  id: Id;
  labelKey: string;
  Icon: SvgIconComponent;
  prefixes: string[];
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
  const macs = devices
    .filter((d) => d.selected && !d.is_manual)
    .map((d) => d.mac.trim().toUpperCase())
    .filter((mac) => mac !== "");
  return [enabled, wisb, [...new Set(macs)].sort()];
};

const nothing = () => [];

// Convention: blocks mirror the B4Section/B4IntegrationCard layout of the section.
// New config subtree inside an existing block → append its prefix here,
// otherwise the path falls back to the section level in the unsaved-changes dialog.
export const CORE_SECTIONS: SettingsSection<CoreSectionId>[] = [
  {
    id: "engine",
    labelKey: "settings.coreTabs.engine",
    prefixes: ["queue", "system.ip_health", "system.dns.keep_ipv6_answers"],
    blocks: [
      { prefixes: ["system.ip_health"], labelKey: "settings.IPHealth.title" },
      { prefixes: ["queue"], labelKey: "settings.Queue.title" },
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

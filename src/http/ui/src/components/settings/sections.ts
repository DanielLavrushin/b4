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

export type CoreSectionId = "engine" | "devices" | "firewall" | "dns" | "socks5";

export type SystemSectionId = "service" | "web" | "backup";

export interface SettingsSection<Id extends string = string> {
  id: Id;
  labelKey: string;
  Icon: SvgIconComponent;
  pick: (c: B4Config) => unknown[];
  restartPick: (c: B4Config) => unknown[];
}

const engineQueue = (c: B4Config) => ({
  ...c.queue,
  devices: undefined,
  mss_clamp: undefined,
});

const nothing = () => [];

export const CORE_SECTIONS: SettingsSection<CoreSectionId>[] = [
  {
    id: "engine",
    labelKey: "settings.coreTabs.engine",
    Icon: NetworkIcon,
    pick: (c) => [
      engineQueue(c),
      c.system.ip_health,
      c.system.dns?.keep_ipv6_answers,
    ],
    restartPick: (c) => [engineQueue(c), c.system.dns?.keep_ipv6_answers],
  },
  {
    id: "devices",
    labelKey: "settings.coreTabs.devices",
    Icon: DeviceUnknowIcon,
    pick: (c) => [c.queue.devices],
    restartPick: (c) => [c.queue.devices],
  },
  {
    id: "firewall",
    labelKey: "settings.coreTabs.firewall",
    Icon: FilterIcon,
    pick: (c) => [c.system.tables, c.queue.mss_clamp],
    restartPick: (c) => [
      { ...c.system.tables, dscp: undefined },
      c.queue.mss_clamp,
    ],
  },
  {
    id: "dns",
    labelKey: "settings.coreTabs.dns",
    Icon: DnsIcon,
    pick: (c) => [{ ...c.system.dns, keep_ipv6_answers: undefined }],
    restartPick: (c) => [{ ...c.system.dns, keep_ipv6_answers: undefined }],
  },
  {
    id: "socks5",
    labelKey: "settings.coreTabs.socks5",
    Icon: ConnectionIcon,
    pick: (c) => [c.system.socks5],
    restartPick: nothing,
  },
];

export const SYSTEM_SECTIONS: SettingsSection<SystemSectionId>[] = [
  {
    id: "service",
    labelKey: "settings.systemTabs.service",
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
  return index < 0 ? 0 : index;
};

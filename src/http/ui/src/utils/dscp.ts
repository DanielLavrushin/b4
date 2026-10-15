import type {
  B4Config,
  B4SetConfig,
  DSCPConfig,
  DevicesConfig,
  SetDSCPConfig,
  TargetsConfig,
} from "@models/config";
import type { HubWarning } from "@models/hub";

export const DSCP_MAX_VALUE = 63;

export const DSCP_SUGGESTED_VALUES = [7, 31, 6, 25];

export type DscpRefusal =
  | "routing_block"
  | "routing_proxy"
  | "routing_mtproto_ws"
  | "source_devices"
  | "source_interfaces"
  | "device_filter"
  | "no_addresses";

export interface DscpEnvironment {
  global?: DSCPConfig;
  skipSetup: boolean;
  deviceFilter: boolean;
  otherValues: number[];
}

const REFUSAL_KEYS: Record<DscpRefusal, string> = {
  routing_block: "sets.routing.dscpOffBlock",
  routing_proxy: "sets.routing.dscpOffProxy",
  routing_mtproto_ws: "sets.routing.dscpOffTelegram",
  source_devices: "sets.routing.dscpOffDevices",
  source_interfaces: "sets.routing.dscpOffSourceIfaces",
  device_filter: "sets.routing.dscpOffDeviceFilter",
  no_addresses: "sets.routing.dscpNoAddresses",
};

const anyNonBlank = (values?: string[]) =>
  (values ?? []).some((value) => value.trim() !== "");

const anyListed = (...lists: (string[] | undefined)[]) =>
  lists.some((list) => (list?.length ?? 0) > 0);

const declaresTargets = (t: TargetsConfig) =>
  anyListed(t.sni_domains, t.ip, t.geosite_categories, t.geoip_categories, t.asns);

const declaresIPTargets = (t: TargetsConfig) =>
  anyListed(t.ip, t.geoip_categories, t.asns);

const routingModeRefusal = (mode: string | undefined): DscpRefusal | null => {
  switch (mode) {
    case "block":
      return "routing_block";
    case "proxy":
      return "routing_proxy";
    case "mtproto-ws":
      return "routing_mtproto_ws";
    default:
      return null;
  }
};

export const deviceFilterSelects = (devices?: DevicesConfig) =>
  !!devices?.enabled && (devices.devices ?? []).some((d) => d.selected);

export const dscpRefusal = (
  set: B4SetConfig,
  deviceFilter: boolean,
): DscpRefusal | null => {
  const { routing, targets } = set;
  const modeRefusal = routing?.enabled ? routingModeRefusal(routing.mode) : null;
  if (modeRefusal) return modeRefusal;
  if (anyNonBlank(targets.source_devices)) return "source_devices";
  if (routing?.enabled && anyNonBlank(routing.source_interfaces)) {
    return "source_interfaces";
  }
  if (deviceFilter) return "device_filter";
  if (
    !declaresTargets(targets) ||
    (targets.domain_only && !declaresIPTargets(targets))
  ) {
    return "no_addresses";
  }
  return null;
};

export const dscpRefusalKey = (set: B4SetConfig, refusal: DscpRefusal) =>
  refusal === "no_addresses" &&
  set.targets.domain_only &&
  declaresTargets(set.targets)
    ? "sets.routing.dscpDomainOnly"
    : REFUSAL_KEYS[refusal];

export const dscpRefusalText = (
  set: B4SetConfig,
  deviceFilter: boolean,
  t: (key: string) => string,
): string | undefined => {
  if (!set.dscp?.enabled) return undefined;
  const refusal = dscpRefusal(set, deviceFilter);
  return refusal ? t(dscpRefusalKey(set, refusal)) : undefined;
};

export const suggestDscpValue = (used: number[], reserved: number[] = []) =>
  DSCP_SUGGESTED_VALUES.find(
    (value) => !used.includes(value) && !reserved.includes(value),
  ) ??
  DSCP_SUGGESTED_VALUES.find((value) => !reserved.includes(value)) ??
  DSCP_SUGGESTED_VALUES[0];

export const setDscpValues = (sets?: B4SetConfig[], exceptId?: string) =>
  (sets ?? []).flatMap((set) =>
    set.id !== exceptId && set.dscp?.enabled ? [set.dscp.value] : [],
  );

export const anySetDscp = (sets?: B4SetConfig[]) =>
  (sets ?? []).some((set) => set.enabled && !!set.dscp?.enabled);

export const dscpStampingSets = (config: B4Config) => {
  const deviceFilter = deviceFilterSelects(config.queue.devices);
  return (config.sets ?? []).filter(
    (set) =>
      set.enabled && !!set.dscp?.enabled && !dscpRefusal(set, deviceFilter),
  );
};

export const dscpEnvironment = (
  config: B4Config,
  setId: string,
): DscpEnvironment => ({
  global: config.system.tables.dscp,
  skipSetup: config.system.tables.skip_setup,
  deviceFilter: deviceFilterSelects(config.queue.devices),
  otherValues: setDscpValues(config.sets, setId),
});

export const keepLocalDscp = (
  local: B4SetConfig,
  deviceFilter: boolean,
): { dscp?: SetDSCPConfig; warnings: HubWarning[] } => {
  const dscp = local.dscp;
  if (!dscp?.enabled || !dscpRefusal(local, deviceFilter)) {
    return { dscp, warnings: [] };
  }
  return {
    dscp: { ...dscp, enabled: false },
    warnings: [{ code: "dscp_off", params: { value: dscp.value } }],
  };
};

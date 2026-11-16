import { B4SetConfig } from "@models/config";
import { hasTargets } from "../facets";

const DNS_PORT = "53";

export interface UdpPortRouting {
  ports: string;
  wanted: boolean;
  scoped: boolean;
  active: boolean;
}

export const udpPortRouting = (config: B4SetConfig): UdpPortRouting => {
  const ports = (config.udp.dport_filter ?? "")
    .split(",")
    .map((port) => port.trim())
    .filter((port) => port !== "" && port !== DNS_PORT)
    .join(", ");
  const routing = config.routing;
  const wanted =
    !!routing?.enabled &&
    routing.mode === "proxy" &&
    routing.upstream?.udp === true &&
    !hasTargets(config) &&
    (config.udp.filter_l7?.length ?? 0) === 0 &&
    ports !== "";
  const scoped =
    (routing?.source_interfaces ?? []).some((iface) => iface.trim() !== "") ||
    (!config.targets.source_devices_exclude &&
      (config.targets.source_devices ?? []).some((mac) => mac.trim() !== ""));
  return { ports, wanted, scoped, active: wanted && scoped };
};

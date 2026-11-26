import type { DiagCapability } from "@models/sysinfo";

export interface CapabilityHints {
  install: boolean;
  keenetic: boolean;
  notNeeded: boolean;
}

export const capabilityHints = (
  cap: DiagCapability,
  engineMode: string,
): CapabilityHints => {
  const notNeeded =
    !cap.available && cap.name === "packet_queue" && engineMode === "tun";
  const unmet = !cap.available && !notNeeded;
  return {
    install: unmet && (cap.packages?.length ?? 0) > 0,
    keenetic: unmet && cap.hint === "keenetic_netfilter",
    notNeeded,
  };
};

import type { TFunction } from "i18next";
import { UdpConfig, UDP_L7_DISCORD, UDP_L7_STUN } from "@models/config";

export interface UdpL7Class {
  value: string;
  labelKey: string;
  descriptionKey: string;
}

export const UDP_L7_CLASSES: readonly UdpL7Class[] = [
  {
    value: UDP_L7_DISCORD,
    labelKey: "sets.udp.filterL7Discord",
    descriptionKey: "sets.udp.filterL7DiscordDesc",
  },
  {
    value: UDP_L7_STUN,
    labelKey: "sets.udp.filterL7Stun",
    descriptionKey: "sets.udp.filterL7StunDesc",
  },
];

export const udpL7Class = (value: string): UdpL7Class | undefined =>
  UDP_L7_CLASSES.find((entry) => entry.value === value);

export const udpL7Label = (t: TFunction, value: string): string => {
  const entry = udpL7Class(value);
  return entry ? t(entry.labelKey) : value;
};

export const DISCORD_CALL_PORTS = "19294-19344,50000-50100";

export type CallsMode = "pass" | "only" | "udp";

export type CallsWhich = "all" | typeof UDP_L7_DISCORD | typeof UDP_L7_STUN;

const CALLS_WHICH: readonly CallsWhich[] = ["all", UDP_L7_DISCORD, UDP_L7_STUN];

export interface CallsSession {
  which?: CallsWhich;
  payload?: string;
  left?: boolean;
}

export const isCallsWhich = (value: string): value is CallsWhich =>
  CALLS_WHICH.some((which) => which === value);

export const normalizeUdpL7 = (classes: unknown): string[] => {
  if (!Array.isArray(classes)) return [];
  const out: string[] = [];
  for (const raw of classes) {
    if (typeof raw !== "string") continue;
    const value = raw.trim().toLowerCase();
    if (value && !out.includes(value)) out.push(value);
  }
  return out;
};

export const callsModeOf = (
  udp: Pick<UdpConfig, "filter_stun" | "filter_l7">,
): CallsMode => {
  if (normalizeUdpL7(udp.filter_l7).length > 0) return "only";
  return udp.filter_stun === false ? "udp" : "pass";
};

export const whichOf = (classes: string[]): CallsWhich | undefined => {
  if (classes.some((value) => !udpL7Class(value))) return undefined;
  const discord = classes.includes(UDP_L7_DISCORD);
  const stun = classes.includes(UDP_L7_STUN);
  if (discord && stun) return "all";
  if (discord) return UDP_L7_DISCORD;
  if (stun) return UDP_L7_STUN;
  return undefined;
};

export const classesFor = (which: CallsWhich): string[] => {
  if (which === "all") return [UDP_L7_DISCORD, UDP_L7_STUN];
  return [which];
};

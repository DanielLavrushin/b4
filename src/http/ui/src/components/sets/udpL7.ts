import type { TFunction } from "i18next";
import { UDP_L7_DISCORD, UDP_L7_STUN } from "@models/config";

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

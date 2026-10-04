import { isReservedProbeHost } from "./probeUrl";
import { ipAddressValid } from "./dnsEndpoint";

export const MAX_RUN_PINS = 64;

export type PinsProblemKind = "line" | "address" | "private" | "name" | "tooMany";

export interface PinsProblem {
  kind: PinsProblemKind;
  value: string;
}

const isAddress = (token: string) =>
  /^\d{1,3}(\.\d{1,3}){3}$/.test(token) ||
  (token.includes(":") && /^[0-9a-f:.]+$/i.test(token));

const pinLines = (text: string) =>
  text.split("\n").map((line) =>
    line
      .replace(/#.*/, "")
      .trim()
      .split(/[\s,=]+/)
      .filter(Boolean),
  );

const pinName = (token: string) => token.toLowerCase().replace(/^\*\./, "");

const LABEL = /^[a-z0-9_-]{1,63}$/;

const pinnableName = (name: string) => {
  const host = name.replace(/\.$/, "");
  return (
    host.length > 0 &&
    host.length <= 253 &&
    host.split(".").every((label) => LABEL.test(label))
  );
};

export const pinsToText = (pins?: Record<string, string[]>) => {
  const byAddress = new Map<string, string[]>();
  for (const [domain, addresses] of Object.entries(pins ?? {})) {
    for (const address of addresses) {
      byAddress.set(address, [...(byAddress.get(address) ?? []), domain]);
    }
  }
  return [...byAddress]
    .map(([address, domains]) => `${address} ${domains.join(" ")}`)
    .join("\n");
};

export const parsePins = (text: string) => {
  const parsed = Object.create(null) as Record<string, string[]>;
  for (const tokens of pinLines(text)) {
    const addresses = tokens.filter(isAddress);
    const domains = tokens.filter((token) => !isAddress(token)).map(pinName);
    if (addresses.length === 0 || domains.length === 0) continue;
    for (const domain of domains) {
      parsed[domain] = [...new Set([...(parsed[domain] ?? []), ...addresses])];
    }
  }
  return parsed;
};

export function pinsProblem(text: string): PinsProblem | null {
  for (const tokens of pinLines(text)) {
    if (tokens.length === 0) continue;
    const addresses = tokens.filter(isAddress);
    const names = tokens.filter((token) => !isAddress(token));
    if (addresses.length === 0 || names.length === 0) {
      return { kind: "line", value: tokens.join(" ") };
    }
    const invalid = addresses.find((address) => !ipAddressValid(address));
    if (invalid) return { kind: "address", value: invalid };
    const reserved = addresses.find((address) => isReservedProbeHost(address));
    if (reserved) return { kind: "private", value: reserved };
    const name = names.find((token) => !pinnableName(pinName(token)));
    if (name) return { kind: "name", value: name };
  }
  const total = Object.values(parsePins(text)).reduce(
    (sum, addresses) => sum + addresses.length,
    0,
  );
  return total > MAX_RUN_PINS
    ? { kind: "tooMany", value: String(MAX_RUN_PINS) }
    : null;
}

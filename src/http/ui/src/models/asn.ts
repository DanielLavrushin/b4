import * as ipaddr from "ipaddr.js";

export interface AsnView {
  id: string;
  name: string;
  prefixes: string[];
  updated_at: number;
  source?: string;
  prefix_count: number;
  v4_count: number;
  v6_count: number;
  ipv4_addresses: number;
  ipv6_slash64s: number;
  used_by: string[];
  last_error?: string;
}

export type AsnViews = Record<string, AsnView>;

export interface AsnLookupOrigin {
  id: string;
  name: string;
  cached: boolean;
}

export interface AsnLookup {
  ip: string;
  prefix: string;
  asns: AsnLookupOrigin[];
}

export interface AsnResolveRequest {
  asn: string;
  refresh?: boolean;
}

export const ASN_LARGE_PREFIXES = 2000;
export const ASN_LARGE_IPV4_ADDRESSES = 16777216;

type AsnSize = Pick<AsnView, "prefix_count" | "ipv4_addresses">;

export const isLargeNetwork = (view?: AsnSize | null): boolean =>
  !!view &&
  (view.prefix_count > ASN_LARGE_PREFIXES ||
    view.ipv4_addresses > ASN_LARGE_IPV4_ADDRESSES);

export const isAsnResolved = (view?: AsnView | null): boolean =>
  !!view && view.prefix_count > 0;

export const formatAsn = (id: string): string => `AS${id}`;

export function formatCompactCount(value: number, lng?: string): string {
  try {
    return new Intl.NumberFormat(lng, {
      notation: "compact",
      maximumFractionDigits: 1,
    }).format(value);
  } catch {
    return value.toLocaleString();
  }
}

const RESERVED_ASN_RANGES: ReadonlyArray<readonly [number, number]> = [
  [0, 0],
  [23456, 23456],
  [64496, 131071],
  [4200000000, 4294967295],
];

const MAX_ASN = 4294967295;

export type AsnInput =
  | { kind: "asn"; id: string }
  | { kind: "reserved"; value: string }
  | { kind: "ip"; ip: string }
  | { kind: "invalid"; value: string };

const stripAsnPrefix = (value: string): string => {
  const lower = value.toLowerCase();
  if (lower.startsWith("asn")) return value.slice(3).trim();
  if (lower.startsWith("as")) return value.slice(2).trim();
  return value;
};

export function normalizeAsn(raw: string): string | null {
  const parsed = parseAsnInput(raw);
  return parsed.kind === "asn" ? parsed.id : null;
}

const bareAddress = (value: string): string => {
  if (value.startsWith("[")) {
    const end = value.indexOf("]");
    return end > 0 ? value.slice(1, end) : value;
  }
  const colons = value.split(":").length - 1;
  return colons === 1 ? value.split(":")[0] : value;
};

export function parseAsnInput(raw: string): AsnInput {
  const value = raw.trim();
  const digits = stripAsnPrefix(value);
  if (/^\d+$/.test(digits)) {
    const n = Number(digits);
    if (!Number.isSafeInteger(n) || n > MAX_ASN) {
      return { kind: "invalid", value };
    }
    if (RESERVED_ASN_RANGES.some(([lo, hi]) => n >= lo && n <= hi)) {
      return { kind: "reserved", value };
    }
    return { kind: "asn", id: String(n) };
  }
  const address = bareAddress(value);
  if (
    ipaddr.IPv4.isValidFourPartDecimal(address) ||
    (address.includes(":") && ipaddr.IPv6.isValid(address))
  ) {
    return { kind: "ip", ip: address };
  }
  return { kind: "invalid", value };
}

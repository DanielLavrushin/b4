export const DOMAIN_CATCH_ALL = "regexp:.*";
export const IPV4_CATCH_ALL = "0.0.0.0/0";
export const IPV6_CATCH_ALL = "::/0";

const REGEXP_PREFIX = "regexp:";

const UNIVERSAL_PATTERNS = new Set([
  ".*",
  "^.*$",
  ".+",
  "^.+$",
  "(.*)",
  "^(.*)$",
]);

const IPV4_CATCH_ALL_ALIASES = new Set(["0.0.0.0/0", "0/0"]);

const IPV6_CATCH_ALL_ALIASES = new Set(["::/0", "0::/0", "0:0:0:0:0:0:0:0/0"]);

const ANY_SHORTHANDS = new Set(["*", "**", "*.*", "any", "all", "0/0"]);

export const isDomainCatchAll = (entry: string): boolean => {
  const value = entry.trim().toLowerCase();
  if (!value.startsWith(REGEXP_PREFIX)) return false;
  return UNIVERSAL_PATTERNS.has(value.slice(REGEXP_PREFIX.length).trim());
};

export const ipCatchAllVersion = (entry: string): 4 | 6 | null => {
  const value = entry.trim().toLowerCase();
  if (IPV4_CATCH_ALL_ALIASES.has(value)) return 4;
  if (IPV6_CATCH_ALL_ALIASES.has(value)) return 6;
  return null;
};

export const isIpCatchAll = (entry: string): boolean =>
  ipCatchAllVersion(entry) !== null;

export const ipCatchAllEntries = (ipv6: boolean): string[] =>
  ipv6 ? [IPV4_CATCH_ALL, IPV6_CATCH_ALL] : [IPV4_CATCH_ALL];

const withoutMatches = (
  items: string[],
  predicate: (item: string) => boolean,
): string[] => items.filter((item) => !predicate(item));

export const setDomainCatchAll = (
  items: string[],
  enabled: boolean,
): string[] => {
  const rest = withoutMatches(items, isDomainCatchAll);
  return enabled ? [...rest, DOMAIN_CATCH_ALL] : rest;
};

export const setIpCatchAll = (
  items: string[],
  enabled: boolean,
  ipv6: boolean,
): string[] => {
  const rest = withoutMatches(items, isIpCatchAll);
  return enabled ? [...rest, ...ipCatchAllEntries(ipv6)] : rest;
};

const isAnyShorthand = (raw: string): boolean =>
  ANY_SHORTHANDS.has(raw.trim().toLowerCase());

export const normalizeDomainEntry = (raw: string): string[] => {
  const value = raw.trim();
  if (!value) return [];
  if (isAnyShorthand(value)) return [DOMAIN_CATCH_ALL];
  if (value.toLowerCase().startsWith(REGEXP_PREFIX)) return [value];

  let normalized = value;
  while (normalized.startsWith("*.")) {
    normalized = normalized.slice(2);
  }
  normalized = normalized.replace(/^\.+/, "");

  return normalized ? [normalized] : [];
};

export const normalizeIpEntry = (raw: string, ipv6: boolean): string[] => {
  const value = raw.trim();
  if (!value) return [];
  if (isAnyShorthand(value)) return ipCatchAllEntries(ipv6);

  const version = ipCatchAllVersion(value);
  if (version === 4) return [IPV4_CATCH_ALL];
  if (version === 6) return [IPV6_CATCH_ALL];

  return [value];
};

export type CatchAllNotice = { kind: "catchAll"; values: string[] };
export type WildcardStrippedNotice = {
  kind: "wildcardStripped";
  from: string;
  to: string;
};
export type EntryNotice = CatchAllNotice | WildcardStrippedNotice;

export const domainEntryNotice = (input: string): EntryNotice | null => {
  const value = input.trim();
  if (!value || value.includes(" ")) return null;
  if (isAnyShorthand(value)) {
    return { kind: "catchAll", values: [DOMAIN_CATCH_ALL] };
  }
  if (!value.startsWith("*.")) return null;

  const [normalized] = normalizeDomainEntry(value);
  if (!normalized || normalized === value) return null;
  return { kind: "wildcardStripped", from: value, to: normalized };
};

export const ipEntryNotice = (
  input: string,
  ipv6: boolean,
): CatchAllNotice | null => {
  const value = input.trim();
  if (!value || value.includes(" ")) return null;
  if (!isAnyShorthand(value)) return null;
  return { kind: "catchAll", values: ipCatchAllEntries(ipv6) };
};

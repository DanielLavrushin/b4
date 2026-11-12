import {
  dnsEndpointError,
  ipAddressCanonical,
  ipAddressUnspecified,
  ipAddressValid,
} from "./dnsEndpoint";

export const MAX_SET_RESOLVERS = 8;

export interface ResolverSource {
  doh_url?: string;
  target_dns?: string;
  upstreams?: string[];
}

export interface ResolverFields {
  doh_url: string;
  target_dns: string;
  upstreams: string[] | undefined;
}

export type ResolverError =
  | "http"
  | "hostname"
  | "bare"
  | "unspecified"
  | "credentials"
  | "invalid";

const URL_ENTRY = /^(https?:\/\/)([^/?#]*)(.*)$/i;
const DOH_ENTRY = /^https:\/\//i;
const BRACKETED = /^\[([^\]]+)\](?::\d+)?$/;
const IPV4_PORT = /^(\d{1,3}(?:\.\d{1,3}){3}):\d+$/;
const DEFAULT_DOH_PATH = "/dns-query";

const text = (value: unknown): string =>
  typeof value === "string" ? value.trim() : "";

export const isDohResolver = (entry: string): boolean =>
  DOH_ENTRY.test(entry.trim());

export const isResolverAddress = (entry: string): boolean => {
  const s = entry.trim();
  return ipAddressValid(s) && !ipAddressUnspecified(s);
};

export const resolverLabel = (entry: string): string =>
  entry.trim().replace(DOH_ENTRY, "");

export const resolverKey = (entry: string): string => {
  const s = entry.trim();
  const url = URL_ENTRY.exec(s);
  if (url) return url[1].toLowerCase() + url[2].toLowerCase() + url[3];
  return ipAddressValid(s) ? ipAddressCanonical(s) : s.toLowerCase();
};

export const resolverValue = (entry: string): string => {
  const key = resolverKey(entry);
  const url = URL_ENTRY.exec(key);
  if (!url || !isDohResolver(key) || url[3].startsWith("/")) return key;
  return url[1] + url[2] + DEFAULT_DOH_PATH + url[3];
};

export const resolverList = (dns?: ResolverSource | null): string[] => {
  const stored = dns?.upstreams;
  const first = text(dns?.doh_url) || text(dns?.target_dns);
  const seen = new Set<string>();
  const list: string[] = [];
  for (const raw of [first, ...(Array.isArray(stored) ? stored : [])]) {
    const entry = text(raw);
    const key = resolverKey(entry);
    if (!entry || seen.has(key)) continue;
    seen.add(key);
    list.push(entry);
  }
  return list;
};

export const misplacedResolver = (dns?: ResolverSource | null): string => {
  const target = text(dns?.target_dns);
  return !text(dns?.doh_url) && URL_ENTRY.test(target) ? target : "";
};

export const resolverFields = (list: string[]): ResolverFields => {
  const [first = "", ...rest] = list;
  const doh = isDohResolver(first);
  return {
    doh_url: doh ? first : "",
    target_dns: doh ? "" : first,
    upstreams: rest.length > 0 ? rest : undefined,
  };
};

const decorated = (s: string): boolean => {
  const inner = BRACKETED.exec(s)?.[1] ?? IPV4_PORT.exec(s)?.[1] ?? s;
  const bare = inner.replace(/%.*$/, "");
  return bare !== s && ipAddressValid(bare);
};

const dohError = (s: string, authority: string): ResolverError | null => {
  if (authority.includes("@")) return "credentials";
  const host = authority.replace(/:\d*$/, "");
  if (/^[\d.]+$/.test(host) && !ipAddressValid(host)) return "invalid";
  return dnsEndpointError(s) ? "invalid" : null;
};

export const resolverError = (raw: string): ResolverError | null => {
  const s = raw.trim();
  const url = URL_ENTRY.exec(s);
  if (url) {
    return url[1].toLowerCase() === "http://" ? "http" : dohError(s, url[2]);
  }
  if (ipAddressValid(s)) return ipAddressUnspecified(s) ? "unspecified" : null;
  if (decorated(s)) return "bare";
  return /[a-z]/i.test(s) && dnsEndpointError(s) === "hostname"
    ? "hostname"
    : "invalid";
};

const askedEntry = (entry: string): boolean => {
  const url = URL_ENTRY.exec(entry);
  if (url) return (url[2] + url[3]).trim() !== "";
  return ipAddressValid(entry);
};

export const askedResolvers = (dns?: ResolverSource | null): string[] =>
  resolverList(misplacedResolver(dns) ? { upstreams: dns?.upstreams } : dns)
    .filter(askedEntry)
    .slice(0, MAX_SET_RESOLVERS);

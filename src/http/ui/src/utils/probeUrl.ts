import * as ipaddr from "ipaddr.js";

export const MAX_PROBE_URLS = 5;

export interface ProbeUrl {
  url: string;
  host: string;
}

const URL_SHAPE = /^([A-Za-z][A-Za-z0-9+.-]*):\/\/([^/?#]*)([^?#]*)(\?[^#]*)?(#.*)?$/;
const HOST_CHARS = /^[A-Za-z0-9\-._~!$&'()*+,;=\u0080-\uffff]+$/;
const PORT = /^\d*$/;
const BAD_ESCAPE = /%(?![0-9A-Fa-f]{2})/;
const PATH_ESCAPE = /[^\x21-\x7e]|["<>\\^`{|}]/gu;
const NON_ASCII = /[^\x20-\x7e]/gu;
const ASCII_ESCAPE = /%[0-7][0-9A-Fa-f]/;

function hasControl(s: string): boolean {
  for (const ch of s) {
    const code = ch.codePointAt(0) ?? 0;
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

function reservedV4(o: number[]): boolean {
  const [a, b] = o;
  if (a === 0 || a === 10 || a === 127 || a >= 224) return true;
  if (a === 100 && b >= 64 && b <= 127) return true;
  if (a === 169 && b === 254) return true;
  if (a === 172 && b >= 16 && b <= 31) return true;
  return a === 192 && b === 168;
}

function reservedV6(parts: number[]): boolean {
  if (parts.every((p) => p === 0)) return true;
  if (parts.slice(0, 7).every((p) => p === 0) && parts[7] === 1) return true;
  const head = parts[0];
  return (
    (head & 0xffc0) === 0xfe80 ||
    (head & 0xff00) === 0xff00 ||
    (head & 0xfe00) === 0xfc00
  );
}

export function isReservedProbeHost(raw: string): boolean {
  const host = raw
    .trim()
    .replace(/^\[|\]$/g, "")
    .toLowerCase()
    .replace(/\.$/, "");
  if (!host) return false;
  if (host === "localhost") return true;
  if (ipaddr.IPv4.isValidFourPartDecimal(host)) {
    return reservedV4(ipaddr.IPv4.parse(host).octets);
  }
  if (ipaddr.IPv6.isValid(host)) {
    const addr = ipaddr.IPv6.parse(host);
    if (addr.isIPv4MappedAddress()) {
      return reservedV4(addr.toIPv4Address().octets);
    }
    return reservedV6(addr.parts);
  }
  return false;
}

function splitAuthority(
  authority: string,
): { host: string; port: string; v6: boolean } | null {
  if (authority.startsWith("[")) {
    const end = authority.indexOf("]");
    if (end < 0) return null;
    const rest = authority.slice(end + 1);
    if (rest && !(rest.startsWith(":") && PORT.test(rest.slice(1)))) {
      return null;
    }
    const host = authority.slice(1, end);
    if (!ipaddr.IPv6.isValid(host)) return null;
    return { host, port: rest.slice(1), v6: true };
  }
  if (authority.split(":").length > 2 && ipaddr.IPv6.isValid(authority)) {
    return { host: authority, port: "", v6: true };
  }
  const colon = authority.lastIndexOf(":");
  const raw = colon < 0 ? authority : authority.slice(0, colon);
  const port = colon < 0 ? "" : authority.slice(colon + 1);
  if (!PORT.test(port)) return null;
  const host = decodeHost(raw);
  if (host === null || (host && !HOST_CHARS.test(host))) return null;
  return { host, port, v6: false };
}

function decodeHost(raw: string): string | null {
  if (!raw.includes("%")) return raw;
  if (ASCII_ESCAPE.test(raw)) return null;
  try {
    return decodeURIComponent(raw);
  } catch {
    return null;
  }
}

function safeDecode(value: string): string {
  try {
    return decodeURI(value);
  } catch {
    return value;
  }
}

interface ProbeParts {
  scheme: string;
  host: string;
  v6: boolean;
  port: string;
  path: string;
  query: string;
}

function parseProbeUrl(raw: string): ProbeParts | null {
  let s = raw
    .trim()
    .replace(/^["'`]+|["'`]+$/g, "")
    .trim();
  if (!s || hasControl(s) || /\s/.test(s)) return null;
  if (!s.includes("://")) s = `https://${s}`;
  const m = URL_SHAPE.exec(s);
  if (!m) return null;
  const scheme = m[1].toLowerCase();
  if (scheme !== "http" && scheme !== "https") return null;
  const authority = m[2];
  if (authority.includes("@")) return null;
  const parts = splitAuthority(authority);
  if (!parts) return null;
  const host = parts.host.toLowerCase().replace(/\.$/, "");
  if (!host || isReservedProbeHost(host)) return null;
  const rawPath = m[3] ?? "";
  if (BAD_ESCAPE.test(rawPath)) return null;
  const path = rawPath
    ? rawPath.replace(PATH_ESCAPE, (c) => encodeURIComponent(c))
    : "/";
  const query = m[4] && m[4].length > 1 ? m[4] : "";
  return { scheme, host, v6: parts.v6, port: parts.port, path, query };
}

function formatProbeUrl(p: ProbeParts, host: string): string {
  const hostPart = p.v6 ? `[${host}]` : host;
  const port = p.port ? `:${p.port}` : "";
  return `${p.scheme}://${hostPart}${port}${p.path}${p.query}`;
}

export function normalizeProbeUrl(raw: string): ProbeUrl | null {
  const p = parseProbeUrl(raw);
  if (!p) return null;
  const host = p.host.replace(NON_ASCII, (c) => encodeURIComponent(c));
  return { url: formatProbeUrl(p, host), host: p.host };
}

export function probeUrlDisplay(url: string): string {
  const p = parseProbeUrl(url);
  if (!p) return url;
  return formatProbeUrl({ ...p, path: safeDecode(p.path) }, p.host);
}

export function sanitizeProbeUrls(list: readonly string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of list) {
    if (out.length >= MAX_PROBE_URLS) break;
    const probe = normalizeProbeUrl(raw);
    if (!probe || seen.has(probe.host)) continue;
    seen.add(probe.host);
    out.push(probe.url);
  }
  return out;
}

export function probeUrlLabel(url: string): string {
  const p = parseProbeUrl(url);
  if (!p) return url;
  if (p.scheme === "https" && p.path === "/" && !p.query) {
    const host = p.v6 ? `[${p.host}]` : p.host;
    return p.port ? `${host}:${p.port}` : host;
  }
  return probeUrlDisplay(url);
}

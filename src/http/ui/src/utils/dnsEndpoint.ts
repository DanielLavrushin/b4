import * as ipaddr from "ipaddr.js";

export type DnsEndpointError = "hostname" | "http" | "unsupported" | "invalid";

const SCHEME = /^([A-Za-z][A-Za-z0-9+.-]*):\/\/(.*)$/;
const IPV4_PORT = /^(\d{1,3}(?:\.\d{1,3}){3})(?::(\d+))?$/;
const BRACKETED = /^\[([^\]]+)\](?::(\d+))?$/;
const HOST_PORT = /^[A-Za-z0-9.-]+(?::\d+)?$/;

function validPort(port?: string): boolean {
  if (port === undefined) return true;
  const n = Number(port);
  return /^\d+$/.test(port) && n >= 1 && n <= 65535;
}

function ipv6Valid(s: string): boolean {
  if (!ipaddr.IPv6.isValid(s) || s.includes("%")) return false;
  const dotted = s.includes(".") ? s.slice(s.lastIndexOf(":") + 1) : "";
  return !dotted || ipaddr.IPv4.isValidFourPartDecimal(dotted);
}

export function ipAddressValid(s: string): boolean {
  return s.includes(":") ? ipv6Valid(s) : ipaddr.IPv4.isValidFourPartDecimal(s);
}

function unspecifiedV6(s: string): boolean {
  const addr = ipaddr.IPv6.parse(s);
  return (
    addr.range() === "unspecified" ||
    (addr.isIPv4MappedAddress() &&
      addr.toIPv4Address().toString() === "0.0.0.0")
  );
}

function plain(raw: string): DnsEndpointError | null {
  const s = raw.trim().replace(/\/$/, "");
  if (!s) return "invalid";
  const v4 = IPV4_PORT.exec(s);
  if (v4) {
    if (!ipaddr.IPv4.isValidFourPartDecimal(v4[1])) return "invalid";
    if (v4[1] === "0.0.0.0" || !validPort(v4[2])) return "invalid";
    return null;
  }
  const bracketed = BRACKETED.exec(s);
  const v6 = bracketed ? bracketed[1] : s;
  if (ipaddr.IPv6.isValid(v6)) {
    if (!ipv6Valid(v6) || unspecifiedV6(v6)) return "invalid";
    return bracketed && !validPort(bracketed[2]) ? "invalid" : null;
  }
  return HOST_PORT.test(s) ? "hostname" : "invalid";
}

function dohURLError(s: string, rest: string): DnsEndpointError | null {
  const authority = rest.split(/[/?#]/, 1)[0];
  if (!authority || /[@\\%]/.test(authority) || rest.includes("\\")) {
    return "invalid";
  }
  try {
    const url = new URL(s);
    if (!url.hostname || url.port === "0") return "invalid";
    decodeURIComponent(url.pathname);
    return null;
  } catch {
    return "invalid";
  }
}

export function dnsEndpointError(raw: string): DnsEndpointError | null {
  const s = raw.trim();
  if (!s) return null;
  const m = SCHEME.exec(s);
  if (!m) return plain(s);
  switch (m[1].toLowerCase()) {
    case "udp":
    case "tcp":
    case "tcp+udp":
      return plain(m[2]);
    case "https":
      return dohURLError(s, m[2]);
    case "http":
      return "http";
    case "tls":
    case "dot":
    case "quic":
    case "doq":
    case "h3":
    case "sdns":
      return "unsupported";
    default:
      return "invalid";
  }
}

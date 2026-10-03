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
    if (v6.includes("%") || ipaddr.IPv6.parse(v6).range() === "unspecified")
      return "invalid";
    return bracketed && !validPort(bracketed[2]) ? "invalid" : null;
  }
  return HOST_PORT.test(s) ? "hostname" : "invalid";
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
      try {
        const url = new URL(s);
        return url.hostname && !url.username && !url.password
          ? null
          : "invalid";
      } catch {
        return "invalid";
      }
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

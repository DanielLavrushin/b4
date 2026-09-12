import { B4SetConfig } from "./config";

export interface HubPayload {
  sha256: string;
  protocol: "tls" | "quic";
  domain?: string;
  size: number;
  data: string;
}

export interface HubEnvelope {
  format: number;
  b4_version: string;
  min_b4_version: string;
  title: string;
  description?: string;
  engine?: string;
  geo?: { site_url?: string; ip_url?: string };
  set: Record<string, unknown>;
  payloads?: HubPayload[];
  fingerprint: string;
  derived_from?: { id: string; version?: number };
}

export interface HubStripped {
  path: string;
  reason: string;
}

export interface HubWarning {
  code: string;
  params?: Record<string, unknown>;
}

export interface HubReport {
  stripped: HubStripped[];
  warnings: HubWarning[];
}

export interface HubEnvelopeResponse {
  envelope: HubEnvelope;
  report: HubReport;
}

export interface HubInstalledPayload {
  protocol: string;
  domain: string;
  file: string;
  size: number;
}

export interface HubImportResponse {
  set: B4SetConfig;
  warnings: HubWarning[];
  payloads: HubInstalledPayload[];
}

export function isHubEnvelope(raw: unknown): raw is HubEnvelope {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return false;
  const r = raw as Record<string, unknown>;
  return "format" in r && "set" in r;
}

function scalarText(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (typeof value === "object" && "path" in value) {
    return scalarText((value).path);
  }
  return JSON.stringify(value);
}

export function formatWarningParam(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map(scalarText).join(", ");
  }
  return scalarText(value);
}

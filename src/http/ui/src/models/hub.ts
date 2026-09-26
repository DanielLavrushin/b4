import { B4SetConfig, HubState, HubVote } from "./config";
import { createDefaultSet } from "./defaults";
import { DomainReassignment, SetDomainMatch } from "./sets";

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

export type HubBucket = "asn" | "country" | "global" | "none";

export interface HubDisplayed {
  bucket: HubBucket;
  score: number;
  n: number;
  devices: number;
  newest?: string;
}

export interface HubBlobRef {
  sha256: string;
  protocol: string;
  domain?: string;
  size: number;
}

export interface HubTargets {
  domains: string[];
  geosite: string[];
  geoip: string[];
  asns: string[];
  ip_count: number;
}

export interface HubMatch {
  entry: string;
  relation: string;
  via: "domain" | "category";
}

export interface HubApplied {
  set_id: string;
  set_name: string;
  hub_state: HubState;
  version: number;
  vote?: HubVote;
  voted_at?: string;
}

export type HubFlag =
  | "needs_payload"
  | "block"
  | "has_pins"
  | "blanket"
  | "catch_all";

export interface HubSet {
  id: string;
  version: number;
  fp: string;
  title: string;
  description?: string;
  author: string;
  b4_min: string;
  b4_version?: string;
  engine?: string;
  family?: string;
  flags: string[];
  status: string;
  created_at: string;
  updated_at: string;
  geo: { site_url?: string; ip_url?: string } | null;
  set: Record<string, unknown>;
  payloads: HubBlobRef[];
  targets: HubTargets;
  display: HubDisplayed;
  match: HubMatch | null;
  applied: HubApplied | null;
}

export interface HubSetsResponse {
  sets: HubSet[];
  total: number;
}

export interface HubCatalogueStatus {
  epoch: number;
  seq: number;
  generated_at: string;
  expires_at: string;
  sets: number;
  expired: boolean;
}

export interface HubStatus {
  enabled: boolean;
  configured: boolean;
  key_id: string;
  last_sync: string;
  last_error: string;
  catalogue: HubCatalogueStatus | null;
  urls: string[];
  mirrors: string[];
  active?: string;
  self_bypass?: boolean;
  set_matches?: SetDomainMatch[];
  network: { asn: string; cc: string; name?: string; source?: string };
  outbox: number;
  hub_key?: string;
  hub_key_builtin?: boolean;
}

export interface HubApplyResponse {
  id: string;
  name: string;
  moved: DomainReassignment[];
  warnings: HubWarning[];
  payloads: HubInstalledPayload[];
}

export type HubVoteKind = HubVote;

export interface HubVoteResponse {
  queued: boolean;
  sent: boolean;
}

export interface HubReportResponse {
  queued: boolean;
  sent: boolean;
}

export interface HubShareResponse {
  hub_id: string;
  version: number;
  status: string;
}

export interface HubIdentity {
  key_id: string;
  created_at: string;
}

export interface HubProbeResult {
  ok: boolean;
  status: string;
  detail: string;
}

export interface HubTestResponse {
  domain: string;
  through_b4: HubProbeResult;
  bypassed: HubProbeResult;
}

export function hubGateStatus(enabled: boolean): HubStatus {
  return {
    enabled,
    configured: false,
    key_id: "",
    last_sync: "",
    last_error: "",
    catalogue: null,
    urls: [],
    mirrors: [],
    network: { asn: "", cc: "" },
    outbox: 0,
  };
}

type PlainObject = Record<string, unknown>;

function isPlainObject(v: unknown): v is PlainObject {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function mergeWithDefaults(partial: unknown, defaults: unknown): unknown {
  if (partial === undefined || partial === null) return defaults;
  if (Array.isArray(defaults)) {
    return Array.isArray(partial) ? partial : defaults;
  }
  if (isPlainObject(defaults)) {
    if (!isPlainObject(partial)) return defaults;
    const merged: PlainObject = { ...defaults };
    for (const [key, value] of Object.entries(partial)) {
      merged[key] =
        key in merged ? mergeWithDefaults(value, merged[key]) : value;
    }
    return merged;
  }
  return partial;
}

export function projectionToSet(
  projection: Record<string, unknown>,
  title: string,
): B4SetConfig {
  const defaults = createDefaultSet(0) as unknown as PlainObject;
  const merged = mergeWithDefaults(projection, defaults) as B4SetConfig;
  merged.id = "";
  merged.name = title || merged.name;
  merged.enabled = true;
  return merged;
}

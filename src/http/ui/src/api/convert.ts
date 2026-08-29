import { apiFetch, apiPost } from "./apiClient";
import { B4SetConfig } from "@models/config";

export type ConvertStatus =
  | "mapped"
  | "approximated"
  | "unsupported"
  | "not_applicable"
  | "degenerate"
  | "unknown"
  | "invalid";

export interface ConvertToolInfo {
  tool: string;
  label: string;
  homepage: string;
  versions: string[];
}

export interface ConvertNote {
  token: string;
  profile: number;
  status: ConvertStatus;
  reason: string;
  fields?: string[];
  params?: Record<string, unknown>;
}

export interface ConvertWarning {
  code: string;
  params?: Record<string, unknown>;
}

export interface ConvertUnresolved {
  kind: string;
  path: string;
  profile: number;
}

export interface ConvertFidelity {
  mapped: number;
  approximated: number;
  unsupported: number;
  not_applicable: number;
  degenerate: number;
  unknown: number;
  invalid: number;
  total: number;
  score: number;
}

export interface ConvertSetPlan {
  profile: number;
  name: string;
  role: "entry" | "fallback";
  fallback_for: number;
  accepts_targets: boolean;
  domains: string[];
  ips: string[];
  strategy: string;
  faking: boolean;
  enabled: boolean;
}

export interface ConvertResult {
  tool: string;
  tool_label: string;
  version: string;
  version_label: string;
  version_inferred: boolean;
  confidence: number;
  argv: string[];
  sets: B4SetConfig[];
  notes: ConvertNote[];
  warnings: ConvertWarning[];
  unresolved: ConvertUnresolved[];
  fidelity: ConvertFidelity;
  plan: ConvertSetPlan[];
  applicable: boolean;
}

export interface ConvertDomainMove {
  domain: string;
  set_name: string;
  set_id: string;
}

export interface ConvertApplyResult extends ConvertResult {
  moved_from?: ConvertDomainMove[];
}

export interface ConvertRequest {
  text: string;
  tool?: string;
  version?: string;
  name_prefix?: string;
  domains?: string[];
  profile_domains?: Record<string, string[]>;
}

export const convertApi = {
  getTools: () => apiFetch<ConvertToolInfo[]>("/api/convert/tools"),
  analyze: (req: ConvertRequest) =>
    apiPost<ConvertResult>("/api/convert/analyze", req),
  apply: (req: ConvertRequest) =>
    apiPost<ConvertApplyResult>("/api/convert/apply", req),
};

export const CONVERT_STATUS_ORDER: ConvertStatus[] = [
  "invalid",
  "unknown",
  "unsupported",
  "degenerate",
  "approximated",
  "mapped",
  "not_applicable",
];

export interface WatchdogDomainStatus {
  domain: string;
  status: "healthy" | "degraded" | "escalating" | "queued";
  last_check: string;
  last_failure?: string;
  last_heal?: string;
  consecutive_failures: number;
  interval_sec: number;
  cooldown_until?: string;
  last_error?: string;
  last_speed?: number;
  matched_set?: string;
  matched_set_id?: string;
  display_domain?: string;
  owner_set_id?: string;
  owner_set_name?: string;
  watched_by_set_id?: string;
  watched_by_set_name?: string;
}

export type WatchdogCheckOutcome = "scheduled" | "master_off" | "healing";

export interface WatchdogSetActionResult {
  success?: boolean;
  message?: string;
  outcome?: WatchdogCheckOutcome;
}

export type SetWatchState =
  | "queued"
  | "healthy"
  | "degraded"
  | "heal_queued"
  | "healing"
  | "cooldown"
  | "unverifiable"
  | "gave_up";

export type URLWatchState =
  | "queued"
  | "ok"
  | "failed"
  | "not_owned"
  | "escalated"
  | "unusable";

export type WatchReason =
  | "not_owned"
  | "escalated"
  | "unusable"
  | "current_works"
  | "not_needed"
  | "partial"
  | "none"
  | "incomplete"
  | "budget"
  | "start_failed"
  | "verify_failed"
  | "edited"
  | "busy";

export interface URLWatchStatus {
  url: string;
  host: string;
  status: URLWatchState;
  owner_set_id?: string;
  owner_set_name?: string;
  escalated_to?: string;
  status_code?: number;
  bytes_read?: number;
  speed?: number;
  last_error?: string;
  last_check?: string;
}

export interface SetWatchStatus {
  set_id: string;
  set_name: string;
  status: SetWatchState;
  reason?: WatchReason;
  urls: URLWatchStatus[];
  consecutive_failures: number;
  heal_failures: number;
  interval_sec: number;
  last_check?: string;
  last_heal?: string;
  last_heal_preset?: string;
  cooldown_until?: string;
  last_error?: string;
}

export interface WatchdogState {
  enabled: boolean;
  domains: WatchdogDomainStatus[];
  sets?: SetWatchStatus[];
}

export type WatchTone = "default" | "info" | "success" | "warning" | "error";

export const SET_WATCH_TONE: Record<SetWatchState, WatchTone> = {
  queued: "default",
  healthy: "success",
  degraded: "warning",
  heal_queued: "info",
  healing: "info",
  cooldown: "warning",
  unverifiable: "default",
  gave_up: "error",
};

export const URL_WATCH_TONE: Record<URLWatchState, WatchTone> = {
  queued: "default",
  ok: "success",
  failed: "error",
  not_owned: "warning",
  escalated: "info",
  unusable: "default",
};

export function setWatchTone(status: string | undefined): WatchTone {
  if (!status) return "default";
  return SET_WATCH_TONE[status as SetWatchState] ?? "default";
}

export function urlWatchTone(status: string | undefined): WatchTone {
  if (!status) return "default";
  return URL_WATCH_TONE[status as URLWatchState] ?? "default";
}

export type SetWatchBlock =
  | "set_disabled"
  | "routed_set"
  | "no_urls"
  | "device_scoped"
  | "ip_only";

interface WatchableSet {
  enabled: boolean;
  routing?: { enabled?: boolean };
  discovery?: { urls?: string[] };
  targets?: {
    sni_domains?: string[] | null;
    geosite_categories?: string[] | null;
    source_devices?: string[] | null;
    source_devices_exclude?: boolean;
  };
}

export function setWatchBlock(set: WatchableSet): SetWatchBlock | null {
  if (set.routing?.enabled) return "routed_set";
  if (
    (set.targets?.source_devices ?? []).length > 0 &&
    !set.targets?.source_devices_exclude
  ) {
    return "device_scoped";
  }
  if (!set.enabled) return "set_disabled";
  if ((set.discovery?.urls ?? []).length === 0) return "no_urls";
  if (
    (set.targets?.sni_domains ?? []).length === 0 &&
    (set.targets?.geosite_categories ?? []).length === 0
  ) {
    return "ip_only";
  }
  return null;
}

export function watchBlockClearsFlag(block: SetWatchBlock | null): boolean {
  return block === "routed_set" || block === "device_scoped";
}

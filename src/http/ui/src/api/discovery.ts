import { apiDelete, apiPost, apiGet } from "./apiClient";
import { B4SetConfig } from "@b4.sets";
import { DiscoveryResponse, DiscoverySuite, HistoryEntry } from "@b4.discovery";

export const discoveryApi = {
  start: (
    check_urls: string[],
    skip_dns: boolean,
    skip_cache: boolean,
    payload_files?: string[],
    validation_tries?: number,
    tls_version?: string,
    ip_version?: string,
  ) =>
    apiPost<DiscoveryResponse>("/api/discovery/start", {
      check_urls,
      skip_dns,
      skip_cache,
      payload_files: payload_files ?? [],
      validation_tries: validation_tries ?? 1,
      tls_version: tls_version ?? "auto",
      ip_version: ip_version ?? "auto",
    }),
  status: (id: string) => apiGet<DiscoverySuite>(`/api/discovery/status/${id}`),
  cancel: (id: string) => apiDelete(`/api/discovery/cancel/${id}`),
  addPresetAsSet: (preset: B4SetConfig) =>
    apiPost<B4SetConfig>("/api/discovery/add", preset),
  clearCache: () => apiPost("/api/discovery/cache/clear", {}),
  current: () => apiGet<DiscoverySuite | null>("/api/discovery/current"),
  history: () => apiGet<HistoryEntry[]>("/api/discovery/history"),
  clearHistory: () => apiPost("/api/discovery/history/clear", {}),
  deleteHistoryDomain: (domain: string) =>
    apiDelete(`/api/discovery/history/${encodeURIComponent(domain)}`),
};

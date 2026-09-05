import { apiDelete, apiPost, apiGet } from "./apiClient";
import { B4SetConfig } from "@models/config";
import {
  DiscoveryCurrent,
  DiscoveryResponse,
  DiscoverySuite,
  HistoryEntry,
  SimilarSet,
} from "@models/discovery";
import { DomainReassignment } from "@models/sets";

export interface AddPresetResult {
  success: boolean;
  message: string;
  moved?: DomainReassignment[];
  id?: string;
  name?: string;
}

export interface DiscoveryStartOptions {
  skipDNS: boolean;
  skipCache: boolean;
  payloadFiles: string[];
  validationTries: number;
  tlsVersion: string;
  ipVersion: string;
}

export const discoveryApi = {
  start: (check_urls: string[], options: DiscoveryStartOptions) =>
    apiPost<DiscoveryResponse>("/api/discovery/start", {
      check_urls,
      skip_dns: options.skipDNS,
      skip_cache: options.skipCache,
      payload_files: options.payloadFiles,
      validation_tries: options.validationTries,
      tls_version: options.tlsVersion,
      ip_version: options.ipVersion,
    }),
  status: (id: string) => apiGet<DiscoverySuite>(`/api/discovery/status/${id}`),
  cancel: (id: string) => apiDelete(`/api/discovery/cancel/${id}`),
  addPresetAsSet: (preset: B4SetConfig) =>
    apiPost<AddPresetResult>("/api/discovery/add", preset),
  similar: (set: B4SetConfig) =>
    apiPost<SimilarSet[]>("/api/discovery/similar", set),
  clearCache: () => apiPost("/api/discovery/cache/clear", {}),
  current: () => apiGet<DiscoveryCurrent>("/api/discovery/current"),
  history: () => apiGet<HistoryEntry[]>("/api/discovery/history"),
  log: () => apiGet<string>("/api/discovery/log", "text"),
  clearHistory: () => apiPost("/api/discovery/history/clear", {}),
  deleteHistoryDomain: (domain: string) =>
    apiDelete(`/api/discovery/history/${encodeURIComponent(domain)}`),
};

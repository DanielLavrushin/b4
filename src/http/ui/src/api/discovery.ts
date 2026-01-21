import { apiDelete, apiPost, apiGet } from "./apiClient";
import { B4SetConfig } from "@b4.sets";
import { DiscoveryResponse, DiscoverySuite } from "@b4.discovery";

export const discoveryApi = {
  start: (check_url: string, skip_dns: boolean, payload_files?: string[], validation_tries?: number) =>
    apiPost<DiscoveryResponse>("/api/discovery/start", {
      check_url,
      skip_dns,
      payload_files: payload_files ?? [],
      validation_tries: validation_tries ?? 1,
    }),
  status: (id: string) => apiGet<DiscoverySuite>(`/api/discovery/status/${id}`),
  cancel: (id: string) => apiDelete(`/api/discovery/cancel/${id}`),
  addPresetAsSet: (preset: B4SetConfig) =>
    apiPost<B4SetConfig>("/api/discovery/add", preset),
};

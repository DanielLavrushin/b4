import { apiGet, apiPost } from "./apiClient";
import { B4SetConfig } from "@models/config";
import {
  HubApplyResponse,
  HubEnvelope,
  HubEnvelopeResponse,
  HubIdentity,
  HubImportResponse,
  HubReportResponse,
  HubSet,
  HubSetsResponse,
  HubShareResponse,
  HubStatus,
  HubTestResponse,
  HubVoteKind,
  HubVoteResponse,
} from "@models/hub";

const setsUrl = (domain: string, limit?: number): string => {
  const params = new URLSearchParams();
  if (domain) params.set("domain", domain);
  if (limit) params.set("limit", String(limit));
  const query = params.toString();
  return `/api/hub/sets${query ? `?${query}` : ""}`;
};

export const hubApi = {
  buildEnvelope: (set: B4SetConfig) =>
    apiPost<HubEnvelopeResponse>("/api/hub/envelope", set),
  importEnvelope: (envelope: HubEnvelope) =>
    apiPost<HubImportResponse>("/api/hub/import", envelope),
  status: () => apiGet<HubStatus>("/api/hub/status"),
  sync: () => apiPost<HubStatus>("/api/hub/sync", {}),
  sets: (domain: string, limit?: number) =>
    apiGet<HubSetsResponse>(setsUrl(domain, limit)),
  set: (id: string) => apiGet<HubSet>(`/api/hub/sets/${encodeURIComponent(id)}`),
  apply: (id: string) =>
    apiPost<HubApplyResponse>(
      `/api/hub/sets/${encodeURIComponent(id)}/apply`,
      {},
    ),
  vote: (id: string, kind: HubVoteKind, domain?: string) =>
    apiPost<HubVoteResponse>(`/api/hub/sets/${encodeURIComponent(id)}/vote`, {
      kind,
      ...(domain ? { domain } : {}),
    }),
  report: (id: string, reason: string) =>
    apiPost<HubReportResponse>(
      `/api/hub/sets/${encodeURIComponent(id)}/report`,
      { reason },
    ),
  test: (id: string, domain?: string) =>
    apiPost<HubTestResponse>(`/api/hub/sets/${encodeURIComponent(id)}/test`, {
      ...(domain ? { domain } : {}),
    }),
  share: (setId: string, description?: string) =>
    apiPost<HubShareResponse>("/api/hub/share", {
      set_id: setId,
      ...(description ? { description } : {}),
    }),
  identity: () => apiGet<HubIdentity>("/api/hub/identity"),
  recoveryCode: () =>
    apiPost<{ code: string }>("/api/hub/identity/recovery", {}),
  restoreIdentity: (code: string) =>
    apiPost<{ key_id: string }>("/api/hub/identity/restore", { code }),
};

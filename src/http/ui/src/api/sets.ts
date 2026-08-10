import { apiDelete, apiFetch, apiPost, apiPut } from "./apiClient";
import { B4SetConfig } from "@b4.sets";

export const setsApi = {
  getSets: () => apiFetch<B4SetConfig[]>("/api/sets"),
  createSet: (set: Omit<B4SetConfig, "id">) =>
    apiPost<B4SetConfig>(`/api/sets`, set),
  updateSet: (id: string, set: B4SetConfig) =>
    apiPut<B4SetConfig>(`/api/sets/${id}`, { ...set, id }),
  deleteSet: (id: string) => apiDelete(`/api/sets/${id}`),
  reorderSets: (set_ids: string[]) =>
    apiPost<void>("/api/sets/reorder", { set_ids }),
  addDomainToSet: (setId: string, domain: string) =>
    apiPost<B4SetConfig>(`/api/sets/${setId}/add-domain`, { domain }),
  deleteSets: (ids: string[]) =>
    apiPost<void>("/api/sets/batch-delete", { ids }),
  setEnabledForSets: (ids: string[], enabled: boolean) =>
    apiPost<void>("/api/sets/batch-set-enabled", { ids, enabled }),
  getTargetedDomains: () => apiFetch<string[]>("/api/sets/targeted-domains"),
  checkDomain: (domain: string) =>
    apiFetch<SetDomainMatch[]>(
      `/api/sets/check-domain?domain=${encodeURIComponent(domain)}`,
    ),
};

export interface SetDomainMatch {
  domain: string;
  set_id: string;
  set_name: string;
  entry: string;
  relation: string;
  via: string;
  enabled: boolean;
}

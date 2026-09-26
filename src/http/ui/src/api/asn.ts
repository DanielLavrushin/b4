import { ApiError, apiDelete, apiGet, apiPost } from "./apiClient";
import {
  AsnLookup,
  AsnResolveRequest,
  AsnView,
  AsnViews,
} from "@models/asn";

export type {
  AsnLookup,
  AsnLookupOrigin,
  AsnView,
  AsnViews,
} from "@models/asn";

export const asnApi = {
  list: async (): Promise<AsnViews> =>
    (await apiGet<AsnViews | null>("/api/asn")) ?? {},
  resolve: (asn: string, refresh = false) =>
    apiPost<AsnView>("/api/asn/resolve", {
      asn,
      refresh,
    } satisfies AsnResolveRequest),
  lookup: (ip: string) =>
    apiGet<AsnLookup>(`/api/asn/lookup?ip=${encodeURIComponent(ip)}`),
  remove: (id: string) => apiDelete(`/api/asn?id=${encodeURIComponent(id)}`),
};

export function asnInUseSets(error: unknown): string[] | null {
  if (!(error instanceof ApiError) || error.code !== "asn_in_use") return null;
  const body = error.body as { used_by?: unknown } | undefined;
  if (!Array.isArray(body?.used_by)) return [];
  return body.used_by.filter((name): name is string => typeof name === "string");
}

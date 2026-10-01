import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { get, put, query } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, KeyDetailView, KeyListView, KeyProfileRequest, NotableKeyView } from "@/models/api";

export interface KeyListQuery {
  q: string;
  status: string;
  tag: string;
  sets: string;
  active: string;
  sort: string;
  dir: string;
  offset: number;
  limit: number;
}

export const useKeyList = (params: KeyListQuery) =>
  useQuery({
    queryKey: qk.keys.rows({ ...params }),
    queryFn: () => get<KeyListView>("/keys" + query({ ...params })),
    placeholderData: keepPreviousData,
  });

export const useKeyDetail = (key: string | null) =>
  useQuery({
    queryKey: qk.keys.detail(key ?? ""),
    queryFn: () => get<KeyDetailView>(`/keys/${encodeURIComponent(key ?? "")}`),
    enabled: key !== null,
  });

export const useNotableKeys = () =>
  useQuery({
    queryKey: qk.keys.notable,
    queryFn: () => get<NotableKeyView[]>("/keys/notable"),
    staleTime: 60_000,
  });

export const useKeyProfile = () =>
  useAdminMutation({
    mutationFn: ({ key, profile }: { key: string; profile: KeyProfileRequest }) => put<ActionResult>(`/keys/${encodeURIComponent(key)}`, profile),
    invalidates: () => [qk.keys.all, qk.audit.all, qk.buildStatus, qk.votes.all],
  });

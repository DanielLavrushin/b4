import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { get, post, query } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, EditPreview, EditRequest, GeoCategoriesView, QueueView, SetDetailView, SetGroupName, SetRowsView, TextEditRequest } from "@/models/api";

export interface SetRowsQuery {
  group: SetGroupName;
  q: string;
  sort: string;
  dir: string;
  offset: number;
  limit: number;
  filter: string;
}

export const useSetRows = (params: SetRowsQuery) =>
  useQuery({
    queryKey: qk.sets.rows({ ...params }),
    queryFn: () => get<SetRowsView>("/sets/rows" + query({ ...params })),
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
  });

export const useQueue = () =>
  useQuery({
    queryKey: qk.sets.queue,
    queryFn: () => get<QueueView>("/queue"),
    refetchInterval: 30_000,
  });

export const useSetDetail = (id: string | null) =>
  useQuery({
    queryKey: qk.sets.detail(id ?? ""),
    queryFn: () => get<SetDetailView>("/sets/" + encodeURIComponent(id ?? "")),
    enabled: id !== null,
  });

export interface EditVariables {
  id: string;
  version: number;
  body: EditRequest;
}

const editPath = (id: string, version: number, action: "preview" | "edit") =>
  `/sets/${encodeURIComponent(id)}/${String(version)}/${action}`;

export const useSetPreview = () =>
  useMutation({
    mutationFn: ({ id, version, body }: EditVariables) => post<EditPreview>(editPath(id, version, "preview"), body),
  });

export const useSetEdit = () =>
  useAdminMutation({
    mutationFn: ({ id, version, body }: EditVariables) => post<ActionResult>(editPath(id, version, "edit"), body),
    invalidates: ({ body }) =>
      body.approve
        ? [qk.sets.all, qk.overview, qk.buildStatus, qk.audit.all, qk.counts, qk.health, qk.votes.all, qk.keys.all, qk.reports.all]
        : [qk.sets.all, qk.audit.all, qk.votes.all, qk.keys.all],
  });

export const useGeoCategories = (enabled: boolean) =>
  useQuery({
    queryKey: qk.geoCategories,
    queryFn: () => get<GeoCategoriesView>("/geo/categories"),
    enabled,
    staleTime: 600_000,
  });

export const useSetText = () =>
  useAdminMutation({
    mutationFn: ({ id, version, body }: { id: string; version: number; body: TextEditRequest }) =>
      post<ActionResult>(`/sets/${encodeURIComponent(id)}/${String(version)}/text`, body),
    invalidates: () => [qk.sets.all, qk.audit.all, qk.buildStatus, qk.votes.all, qk.reports.all],
  });

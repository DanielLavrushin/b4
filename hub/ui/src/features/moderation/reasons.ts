import { useQuery } from "@tanstack/react-query";
import { del, get, post, put } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, ReasonMoveRequest, ReasonPresetRequest, ReasonPresetView } from "@/models/api";

export const useReasonPresets = () => useQuery({ queryKey: qk.reasons, queryFn: () => get<ReasonPresetView[]>("/reasons"), staleTime: 60_000 });

export const useCreatePreset = () =>
  useAdminMutation({
    mutationFn: (req: ReasonPresetRequest) => post<ActionResult>("/reasons", req),
    invalidates: () => [qk.reasons],
  });

export const useUpdatePreset = () =>
  useAdminMutation({
    mutationFn: ({ id, req }: { id: number; req: ReasonPresetRequest }) => put<ActionResult>(`/reasons/${String(id)}`, req),
    invalidates: () => [qk.reasons],
  });

export const useMovePreset = () =>
  useAdminMutation({
    mutationFn: ({ id, req }: { id: number; req: ReasonMoveRequest }) => post<ActionResult>(`/reasons/${String(id)}/move`, req),
    invalidates: () => [qk.reasons],
  });

export const useDeletePreset = () =>
  useAdminMutation({
    mutationFn: (id: number) => del<ActionResult>(`/reasons/${String(id)}`),
    invalidates: () => [qk.reasons],
  });

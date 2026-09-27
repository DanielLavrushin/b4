import { useQuery } from "@tanstack/react-query";
import { get, post } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, MirrorAction, MirrorCheckResult, MirrorsView } from "@/models/api";

export const useMirrors = () =>
  useQuery({
    queryKey: qk.mirrors.all,
    queryFn: () => get<MirrorsView>("/mirrors"),
    refetchInterval: 30_000,
  });

export interface MirrorActionVariables {
  id: number;
  action: MirrorAction;
  reason?: string;
}

export const useMirrorAction = () =>
  useAdminMutation({
    mutationFn: ({ id, action, reason }: MirrorActionVariables) => post<ActionResult>(`/mirrors/${String(id)}/${action}`, { reason: reason ?? "" }),
    invalidates: () => [qk.mirrors.all, qk.overview, qk.buildStatus, qk.audit.all, qk.counts, qk.health],
  });

export const useMirrorCheck = () =>
  useAdminMutation({
    mutationFn: (id: number) => post<MirrorCheckResult>(`/mirrors/${String(id)}/check`),
    invalidates: () => [qk.mirrors.all, qk.overview, qk.buildStatus, qk.audit.all, qk.health],
  });

export const useMirrorsCheck = () =>
  useAdminMutation({
    mutationFn: () => post<ActionResult>("/mirrors/check"),
    invalidates: () => [qk.mirrors.all, qk.overview, qk.buildStatus, qk.health],
  });

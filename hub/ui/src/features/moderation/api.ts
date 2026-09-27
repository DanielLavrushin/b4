import { useQuery } from "@tanstack/react-query";
import { get, post } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, KeyAction, KeyImpactView, ModerationRequest, ModerationView, ReportAction, SetAction } from "@/models/api";

const setPath = (id: string) => `/sets/${encodeURIComponent(id)}`;
const versionPath = (id: string, version: number) => `${setPath(id)}/${String(version)}`;

const afterModeration = [qk.sets.all, qk.overview, qk.reports.all, qk.buildStatus, qk.audit.all, qk.keys.all, qk.preview.all, qk.counts, qk.votes.all, qk.health];

export interface VersionActionVariables {
  id: string;
  version: number;
  action: SetAction;
  reason?: string;
  force?: boolean;
  withdraw?: boolean;
  keepReports?: boolean;
  expectStatus?: string;
}

export const useVersionAction = () =>
  useAdminMutation({
    mutationFn: ({ id, version, action, reason, force, withdraw, keepReports, expectStatus }: VersionActionVariables) =>
      post<ActionResult>(`${versionPath(id, version)}/${action}`, {
        reason: reason ?? "",
        force: force ?? false,
        withdraw: withdraw ?? false,
        keep_reports: keepReports ?? false,
        expect_status: expectStatus ?? "",
      }),
    invalidates: () => afterModeration,
  });

export const usePreview = (request: ModerationRequest | null) =>
  useQuery({
    queryKey: request ? [...qk.preview.all, request] : qk.preview.all,
    queryFn: () => post<ModerationView>("/moderation", { ...request, dry_run: true }),
    enabled: request !== null,
    retry: false,
    staleTime: 0,
  });

export const useModerate = () =>
  useAdminMutation({
    mutationFn: (request: ModerationRequest) => post<ModerationView>("/moderation", request),
    invalidates: () => afterModeration,
  });

export const useWithdraw = () =>
  useAdminMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) => post<ActionResult>(`${setPath(id)}/withdraw`, { reason }),
    invalidates: () => afterModeration,
  });

export const useReinstate = () =>
  useAdminMutation({
    mutationFn: (id: string) => post<ActionResult>(`${setPath(id)}/reinstate`),
    invalidates: () => afterModeration,
  });

export const useSetDelete = () =>
  useAdminMutation({
    mutationFn: (id: string) => post<ActionResult>(`${setPath(id)}/delete`, { confirm: id }),
    invalidates: () => [qk.sets.all, qk.overview, qk.feedback.all, qk.keys.all, qk.reports.all, qk.buildStatus, qk.audit.all, qk.counts, qk.health],
    removes: (id) => [qk.sets.detail(id)],
  });

export interface VersionReportsVariables {
  id: string;
  version: number;
  action: Exclude<ReportAction, "reopen">;
  note?: string;
}

export const useVersionReports = () =>
  useAdminMutation({
    mutationFn: ({ id, version, action, note }: VersionReportsVariables) =>
      post<ActionResult>(`${versionPath(id, version)}/reports/${action}`, { note: note ?? "" }),
    invalidates: () => [qk.sets.all, qk.reports.all, qk.overview, qk.audit.all, qk.counts, qk.health],
  });

export interface KeyActionVariables {
  key: string;
  action: KeyAction;
  reason?: string;
}

export const useKeyAction = () =>
  useAdminMutation({
    mutationFn: ({ key, action, reason }: KeyActionVariables) =>
      post<ActionResult>(`/keys/${encodeURIComponent(key)}/${action}`, { reason: reason ?? "" }),
    invalidates: ({ action }) =>
      action === "ban" || action === "unban" ? afterModeration : [qk.keys.all, qk.overview, qk.audit.all],
  });

export const useKeyImpact = (key: string | null) =>
  useQuery({
    queryKey: qk.keys.impact(key ?? ""),
    queryFn: () => get<KeyImpactView>(`/keys/${encodeURIComponent(key ?? "")}/impact`),
    enabled: key !== null,
    staleTime: 0,
  });

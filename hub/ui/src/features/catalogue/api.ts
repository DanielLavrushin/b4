import { useEffect, useRef } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { get, post, query } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, BuildStateView, BuildsPageView } from "@/models/api";

const busy = (state?: BuildStateView) => state?.state === "queued" || state?.state === "building";

const afterPublish = [qk.overview, qk.health, qk.builds.all, qk.mirrors.all, qk.sets.all, qk.counts];

export const useBuildStatus = () =>
  useQuery({
    queryKey: qk.buildStatus,
    queryFn: () => get<BuildStateView>("/catalogue/status"),
    refetchInterval: (q) => (busy(q.state.data) ? 2_000 : 30_000),
    refetchIntervalInBackground: false,
  });

export function useRefreshOnPublish(state?: BuildStateView) {
  const client = useQueryClient();
  const seen = useRef<string | null>(null);
  const mark = state ? `${String(state.last_ok?.id ?? 0)}/${String(state.last_error?.id ?? 0)}` : null;
  useEffect(() => {
    if (mark === null) return;
    if (seen.current !== null && seen.current !== mark) {
      afterPublish.forEach((queryKey) => void client.invalidateQueries({ queryKey }));
    }
    seen.current = mark;
  }, [mark, client]);
}

export const useBuilds = (only: "" | "changes" | "failed") =>
  useInfiniteQuery({
    queryKey: qk.builds.list(only),
    queryFn: ({ pageParam }) => get<BuildsPageView>("/catalogue/builds" + query({ only, before: pageParam || undefined, limit: 25 })),
    initialPageParam: 0,
    getNextPageParam: (page) => page.next || undefined,
  });

const afterBuild = [qk.overview, qk.mirrors.all, qk.buildStatus, qk.builds.all, qk.audit.all];

export const useCatalogueBuild = () =>
  useAdminMutation({
    mutationFn: () => post<ActionResult>("/catalogue/build", {}),
    invalidates: () => afterBuild,
  });

export const useCatalogueEpoch = () =>
  useAdminMutation({
    mutationFn: () => post<ActionResult>("/catalogue/epoch"),
    invalidates: () => afterBuild,
  });

export interface RevokeVariables {
  keyId: string;
  confirm: string;
  allowBuiltin: boolean;
}

export const useCatalogueRevoke = () =>
  useAdminMutation({
    mutationFn: ({ keyId, confirm, allowBuiltin }: RevokeVariables) =>
      post<ActionResult>("/catalogue/revoke", { key_id: keyId, confirm, allow_builtin: allowBuiltin }),
    invalidates: () => afterBuild,
  });

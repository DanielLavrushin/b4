import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { asnApi } from "@api/asn";
import { AsnView, AsnViews, isAsnResolved } from "@models/asn";

export const asnKeys = {
  all: ["asn"] as const,
};

const UNRESOLVED_POLL_MS = 20 * 1000;

interface AsnViewsOptions {
  enabled?: boolean;
  watchIds?: string[];
}

export function useAsnViews({ enabled = true, watchIds }: AsnViewsOptions = {}) {
  return useQuery({
    queryKey: asnKeys.all,
    queryFn: () => asnApi.list(),
    enabled,
    staleTime: 60 * 1000,
    retry: false,
    refetchInterval: (query) => {
      if (!enabled || !watchIds?.length) return false;
      const views = query.state.data;
      if (!views) return false;
      return watchIds.some((id) => !isAsnResolved(views[id]))
        ? UNRESOLVED_POLL_MS
        : false;
    },
  });
}

export function useAsnCache() {
  const queryClient = useQueryClient();

  const store = useCallback(
    (view: AsnView) => {
      queryClient.setQueryData<AsnViews>(asnKeys.all, (prev) =>
        prev ? { ...prev, [view.id]: view } : prev,
      );
    },
    [queryClient],
  );

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: asnKeys.all }),
    [queryClient],
  );

  return { store, invalidate };
}

import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { get, query } from "@/api/client";
import { qk } from "@/api/queryKeys";
import type { AuditPageView } from "@/models/api";

export interface AuditFilter {
  target_kind: string;
  target_id: string;
  actor: string;
  batch: string;
}

const PAGE = 50;

export const useAuditLog = (filter: AuditFilter) =>
  useInfiniteQuery({
    queryKey: qk.audit.list({ ...filter }),
    queryFn: ({ pageParam }) => get<AuditPageView>("/audit" + query({ ...filter, limit: PAGE, before: pageParam || undefined })),
    initialPageParam: 0,
    getNextPageParam: (last) => last.next || undefined,
    placeholderData: keepPreviousData,
  });

export const useTargetHistory = (kind: string, id: string | null) =>
  useQuery({
    queryKey: qk.audit.list({ target_kind: kind, target_id: id ?? "", recent: true }),
    queryFn: () => get<AuditPageView>("/audit" + query({ target_kind: kind, target_id: id ?? "", limit: PAGE })),
    enabled: id !== null && id !== "",
  });

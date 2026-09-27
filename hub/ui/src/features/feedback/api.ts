import { useInfiniteQuery } from "@tanstack/react-query";
import { get, post, query } from "@/api/client";
import { useAdminMutation } from "@/api/mutations";
import { qk } from "@/api/queryKeys";
import type { ActionResult, ReportAction, ReportsPageView, VotesPageView } from "@/models/api";

export interface VoteFilter {
  set: string;
  key: string;
  sign: string;
  author: string;
  verified: string;
  asn: string;
  cc: string;
}

export const useVotes = (filter: VoteFilter) =>
  useInfiniteQuery({
    queryKey: qk.votes.list({ ...filter }),
    queryFn: ({ pageParam }) => get<VotesPageView>("/votes" + query({ ...filter, before: pageParam || undefined, limit: 100 })),
    initialPageParam: "",
    getNextPageParam: (page) => page.next || undefined,
    refetchInterval: 60_000,
  });

export interface ReportFilter {
  state: string;
  set?: string;
  key?: string;
}

export const useReports = (filter: ReportFilter) =>
  useInfiniteQuery({
    queryKey: qk.reports.list({ ...filter }),
    queryFn: ({ pageParam }) =>
      get<ReportsPageView>("/reports" + query({ state: filter.state, set: filter.set, key: filter.key, before: pageParam || undefined, limit: 100 })),
    initialPageParam: "",
    getNextPageParam: (page) => page.next || undefined,
    refetchInterval: 60_000,
  });

export interface ReportsVariables {
  ids: number[];
  action: ReportAction;
  note?: string;
}

export const useReportsAction = () =>
  useAdminMutation({
    mutationFn: ({ ids, action, note }: ReportsVariables) =>
      ids.length === 1
        ? post<ActionResult>(`/reports/${String(ids[0])}/${action}`, { note: note ?? "" })
        : post<ActionResult>("/reports/bulk", { ids, action, note: note ?? "" }),
    invalidates: () => [qk.reports.all, qk.sets.all, qk.overview, qk.audit.all, qk.counts, qk.health],
  });

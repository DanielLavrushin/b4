import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { get, query } from "@/api/client";
import { qk } from "@/api/queryKeys";
import type { StatsView } from "@/models/api";

export const useStats = (days: number) =>
  useQuery({
    queryKey: qk.stats(days),
    queryFn: () => get<StatsView>("/stats" + query({ days })),
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  });

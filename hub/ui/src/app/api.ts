import { useQuery } from "@tanstack/react-query";
import { get } from "@/api/client";
import { qk } from "@/api/queryKeys";
import type { BadgesView } from "@/models/api";

export const useCounts = () =>
  useQuery({
    queryKey: qk.counts,
    queryFn: () => get<BadgesView>("/counts"),
    refetchInterval: 30_000,
    refetchIntervalInBackground: true,
    staleTime: 10_000,
  });

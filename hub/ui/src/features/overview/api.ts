import { useQuery } from "@tanstack/react-query";
import { get } from "@/api/client";
import { qk } from "@/api/queryKeys";
import type { HealthView, OverviewView } from "@/models/api";

export const useOverview = () =>
  useQuery({
    queryKey: qk.overview,
    queryFn: () => get<OverviewView>("/overview"),
    refetchInterval: 30_000,
  });

export const useHealth = () =>
  useQuery({
    queryKey: qk.health,
    queryFn: () => get<HealthView>("/health"),
    refetchInterval: 60_000,
  });

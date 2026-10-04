import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { mtprotoApi } from "@api/mtproto";

export const telegramBridgeKey = ["mtproto", "bridge"] as const;

export function useTelegramBridgeStatus() {
  return useQuery({
    queryKey: telegramBridgeKey,
    queryFn: () => mtprotoApi.telegramBridge(),
    staleTime: 0,
    refetchInterval: 10 * 1000,
    retry: false,
  });
}

const BRIDGE_SUMMARY_POLL_MS = 60 * 1000;
const BRIDGE_SUMMARY_STALE_MS = 30 * 1000;

export function useTelegramBridgeSummary() {
  return useQuery({
    queryKey: telegramBridgeKey,
    queryFn: () => mtprotoApi.telegramBridge(),
    staleTime: BRIDGE_SUMMARY_STALE_MS,
    refetchInterval: (query) =>
      query.state.data?.enabled ? BRIDGE_SUMMARY_POLL_MS : false,
    retry: false,
  });
}

export function useTelegramBridgeEnabled(): boolean {
  const query = useQuery({
    queryKey: telegramBridgeKey,
    queryFn: () => mtprotoApi.telegramBridge(),
    staleTime: BRIDGE_SUMMARY_STALE_MS,
    retry: false,
    select: (data) => data.enabled,
  });
  return query.data ?? false;
}

export function useCheckTelegramBridge() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => mtprotoApi.telegramBridge(true),
    onSuccess: (data) => client.setQueryData(telegramBridgeKey, data),
  });
}

export function useRefreshTelegramAddresses() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => mtprotoApi.refreshTelegramBridge(),
    onSuccess: (data) => client.setQueryData(telegramBridgeKey, data),
  });
}

export function useTelegramBridgeInvalidate() {
  const client = useQueryClient();
  return () => client.invalidateQueries({ queryKey: telegramBridgeKey });
}

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

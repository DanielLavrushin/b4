import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { get, post, put } from "@/api/client";
import { qk } from "@/api/queryKeys";
import type { LimitsView, NotifyChannel, NotifyRequest, NotifyView, SettingsView } from "@/models/api";

export const useSettings = () => useQuery({ queryKey: qk.settings, queryFn: () => get<SettingsView>("/settings") });

export const useSaveSettings = () => {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (limits: LimitsView) => put<SettingsView>("/settings", { limits }),
    onSuccess: (data) => {
      client.setQueryData(qk.settings, data);
    },
  });
};

export const useNotify = () => useQuery({ queryKey: qk.notify, queryFn: () => get<NotifyView>("/notify") });

export const useSaveNotify = () => {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (req: NotifyRequest) => put<NotifyView>("/notify", req),
    onSuccess: (data) => {
      client.setQueryData(qk.notify, data);
      void client.invalidateQueries({ queryKey: qk.audit.all });
    },
  });
};

export const useTestNotify = () => {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (channel: NotifyChannel) => post<NotifyView>("/notify/test", { channel }),
    onSuccess: (data) => {
      client.setQueryData(qk.notify, data);
    },
    onSettled: () => {
      void client.invalidateQueries({ queryKey: qk.notify });
      void client.invalidateQueries({ queryKey: qk.audit.all });
    },
  });
};

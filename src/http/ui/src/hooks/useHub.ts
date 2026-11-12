import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { hubApi } from "@api/hub";
import { setsApi } from "@api/sets";
import { HubSet, HubVoteKind } from "@models/hub";

export const hubKeys = {
  all: ["hub"] as const,
  status: ["hub", "status"] as const,
  identity: ["hub", "identity"] as const,
  sets: (domain: string) => ["hub", "sets", domain] as const,
  set: (id: string) => ["hub", "set", id] as const,
};

export function useHubStatus(enabled = true) {
  return useQuery({
    queryKey: hubKeys.status,
    queryFn: hubApi.status,
    enabled,
    staleTime: 30 * 1000,
    retry: false,
  });
}

export function useHubSets(domain: string, limit?: number, enabled = true) {
  return useQuery({
    queryKey: hubKeys.sets(domain),
    queryFn: () => hubApi.sets(domain, limit),
    enabled,
    staleTime: 30 * 1000,
    retry: false,
    placeholderData: (previous) => previous,
  });
}

export function useHubSet(id: string | null) {
  return useQuery({
    queryKey: hubKeys.set(id ?? ""),
    queryFn: () => hubApi.set(id ?? ""),
    enabled: Boolean(id),
    retry: false,
  });
}

export function useHubIdentity(enabled = true) {
  return useQuery({
    queryKey: hubKeys.identity,
    queryFn: hubApi.identity,
    enabled,
    retry: false,
  });
}

export function useHubInvalidate() {
  const client = useQueryClient();
  return () => client.invalidateQueries({ queryKey: hubKeys.all });
}

export function useHubSync() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: hubApi.sync,
    onSuccess: (status) => {
      client.setQueryData(hubKeys.status, status);
      void client.invalidateQueries({ queryKey: ["hub", "sets"] });
      void client.invalidateQueries({ queryKey: ["hub", "set"] });
    },
  });
}

export function useHubApply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ set, replace }: { set: HubSet; replace?: string }) =>
      hubApi.apply(set.id, replace),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ["hub", "sets"] });
      void client.invalidateQueries({ queryKey: ["hub", "set"] });
    },
  });
}

export function useHubUndoApply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (localSetId: string) => setsApi.deleteSet(localSetId),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ["hub", "sets"] });
      void client.invalidateQueries({ queryKey: ["hub", "set"] });
    },
  });
}

export function useHubVote() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      kind,
      domain,
    }: {
      id: string;
      kind: HubVoteKind;
      domain?: string;
    }) => hubApi.vote(id, kind, domain),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: hubKeys.status });
      void client.invalidateQueries({ queryKey: ["hub", "sets"] });
      void client.invalidateQueries({ queryKey: ["hub", "set"] });
    },
  });
}

export function useHubReport() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) =>
      hubApi.report(id, reason),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: hubKeys.status });
    },
  });
}

export function useHubTest() {
  return useMutation({
    mutationFn: ({ id, domain }: { id: string; domain?: string }) =>
      hubApi.test(id, domain),
  });
}

export function useHubShare() {
  return useMutation({
    mutationFn: ({
      setId,
      description,
    }: {
      setId: string;
      description?: string;
    }) => hubApi.share(setId, description),
  });
}

export function useHubRecoveryCode() {
  return useMutation({ mutationFn: hubApi.recoveryCode });
}

export function useHubRestoreIdentity() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => hubApi.restoreIdentity(code),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: hubKeys.all });
    },
  });
}

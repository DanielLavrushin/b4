import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { hubApi } from "@api/hub";
import { setsApi } from "@api/sets";
import { HubSet, HubVoteKind } from "@models/hub";
import { DomainReassignment } from "@models/sets";

export const hubKeys = {
  all: ["hub"] as const,
  status: ["hub", "status"] as const,
  sets: (domain: string) => ["hub", "sets", domain] as const,
  set: (id: string) => ["hub", "set", id] as const,
  groupsOf: (setId: string) => ["hub", "groups", setId] as const,
};

const invalidateHubLists = (client: ReturnType<typeof useQueryClient>) => {
  void client.invalidateQueries({ queryKey: ["hub", "sets"] });
  void client.invalidateQueries({ queryKey: ["hub", "set"] });
  void client.invalidateQueries({ queryKey: ["hub", "groups"] });
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

export function useHubGroupsOf(setId: string, enabled = true) {
  return useQuery({
    queryKey: hubKeys.groupsOf(setId),
    queryFn: () => hubApi.groups(setId),
    enabled: enabled && Boolean(setId),
    staleTime: 30 * 1000,
    retry: false,
  });
}

export function useHubGroups(enabled = true) {
  return useQuery({
    queryKey: hubKeys.groupsOf(""),
    queryFn: () => hubApi.groups(),
    enabled,
    staleTime: 30 * 1000,
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
      invalidateHubLists(client);
    },
  });
}

export function useHubApply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      set,
      replace,
      discard,
    }: {
      set: HubSet;
      replace?: string;
      discard?: string;
    }) => hubApi.apply(set.id, replace, discard),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: ["hub", "sets"] });
      void client.invalidateQueries({ queryKey: ["hub", "set"] });
    },
  });
}

export function useHubGroupApply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, replace }: { id: string; replace?: boolean }) =>
      hubApi.applyGroup(id, replace),
    onSettled: () => invalidateHubLists(client),
  });
}

export function useHubGroupShare() {
  return useMutation({
    mutationFn: ({
      group,
      description,
    }: {
      group: string;
      description?: string;
    }) => hubApi.shareGroup(group, description),
  });
}

export function useHubUndoApply() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ ids, moved }: { ids: string[]; moved: DomainReassignment[] }) =>
      setsApi.deleteSets(ids, moved),
    onSettled: () => invalidateHubLists(client),
  });
}

export function useLocalSetsEnabled() {
  return useMutation({
    mutationFn: ({ ids, enabled }: { ids: string[]; enabled: boolean }) =>
      setsApi.setEnabledForSets(ids, enabled),
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

export function useHubGroupVote() {
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
    }) => hubApi.voteGroup(id, kind, domain),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: hubKeys.status });
      invalidateHubLists(client);
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

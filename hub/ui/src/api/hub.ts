import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { get, post } from "./client";
import type {
  ActionResult,
  FeedbackView,
  KeyAction,
  KeyView,
  MirrorAction,
  MirrorView,
  OverviewView,
  SessionState,
  SetAction,
  SetDetailView,
  SetsView,
} from "@/models/api";

export const keys = {
  session: ["session"] as const,
  overview: ["overview"] as const,
  sets: ["sets"] as const,
  set: (id: string) => ["sets", id] as const,
  keys: ["keys"] as const,
  mirrors: ["mirrors"] as const,
  feedback: (limit: number) => ["feedback", limit] as const,
};

export const fetchSession = () => get<SessionState>("/session");
export const login = (password: string) =>
  post<SessionState>("/login", { password });
export const logout = () => post<void>("/logout");

export const useOverview = () =>
  useQuery({
    queryKey: keys.overview,
    queryFn: () => get<OverviewView>("/overview"),
    refetchInterval: 30_000,
  });

export const useSets = () =>
  useQuery({
    queryKey: keys.sets,
    queryFn: () => get<SetsView>("/sets"),
    refetchInterval: 30_000,
  });

export const useSetDetail = (id: string | null) =>
  useQuery({
    queryKey: keys.set(id ?? ""),
    queryFn: () => get<SetDetailView>("/sets/" + encodeURIComponent(id ?? "")),
    enabled: id !== null,
  });

export const useKeys = () =>
  useQuery({ queryKey: keys.keys, queryFn: () => get<KeyView[]>("/keys") });

export const useMirrors = () =>
  useQuery({
    queryKey: keys.mirrors,
    queryFn: () => get<MirrorView[]>("/mirrors"),
    refetchInterval: 30_000,
  });

export const useFeedback = (limit: number) =>
  useQuery({
    queryKey: keys.feedback(limit),
    queryFn: () => get<FeedbackView>("/feedback?limit=" + String(limit)),
  });

const useInvalidatingMutation = <TVariables>(
  mutationFn: (variables: TVariables) => Promise<ActionResult>,
) => {
  const client = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => {
      void client.invalidateQueries();
    },
  });
};

export interface SetActionVariables {
  id: string;
  version: number;
  action: SetAction;
  reason?: string;
}

export const useSetAction = () =>
  useInvalidatingMutation(({ id, version, action, reason }: SetActionVariables) =>
    post<ActionResult>(
      `/sets/${encodeURIComponent(id)}/${String(version)}/${action}`,
      { reason: reason ?? "" },
    ),
  );

export interface KeyActionVariables {
  key: string;
  action: KeyAction;
  reason?: string;
}

export const useKeyAction = () =>
  useInvalidatingMutation(({ key, action, reason }: KeyActionVariables) =>
    post<ActionResult>(`/keys/${encodeURIComponent(key)}/${action}`, {
      reason: reason ?? "",
    }),
  );

export interface MirrorActionVariables {
  id: number;
  action: MirrorAction;
  reason?: string;
}

export const useMirrorAction = () =>
  useInvalidatingMutation(({ id, action, reason }: MirrorActionVariables) =>
    post<ActionResult>(`/mirrors/${String(id)}/${action}`, {
      reason: reason ?? "",
    }),
  );

export const useCatalogueBuild = () =>
  useInvalidatingMutation(() => post<ActionResult>("/catalogue/build"));

export const useCatalogueEpoch = () =>
  useInvalidatingMutation(() => post<ActionResult>("/catalogue/epoch"));

export const useCatalogueRevoke = () =>
  useInvalidatingMutation((keyId: string) =>
    post<ActionResult>("/catalogue/revoke", { key_id: keyId }),
  );

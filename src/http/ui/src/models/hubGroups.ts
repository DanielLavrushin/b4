import type { HubGroup, HubGroupApplyResponse, HubWarning } from "./hub";
import type { CoveredSet, DomainReassignment } from "./sets";

export type HubGroupAction = "apply" | "applied" | "update" | "reapply";

export function hubGroupAction(hubGroup: HubGroup): HubGroupAction {
  switch (hubGroup.applied.state) {
    case "applied":
      return "applied";
    case "update":
      return "update";
    case "modified":
      return "reapply";
    default:
      return "apply";
  }
}

export function hubGroupTakenOver(hubGroup: HubGroup): string[] {
  return hubGroup.members.flatMap((m) =>
    m.replaces ? [m.replaces.set_name] : [],
  );
}

export function hubGroupEscalationTarget(
  hubGroup: HubGroup,
  position: number,
): boolean {
  return hubGroup.members.some((m) => m.escalate?.to === position);
}

export function hubGroupUndo(
  res: HubGroupApplyResponse,
  replace: boolean,
): string[] {
  return replace || res.created.length < res.sets.length ? [] : res.created;
}

export function hubGroupDescriptionOf(
  hubGroups: HubGroup[] | undefined,
  group: string,
): string {
  return (
    hubGroups?.find((hubGroup) => hubGroup.applied.group === group)
      ?.description ?? ""
  );
}

const union = (a: string[] | null, b: string[] | null): string[] => [
  ...new Set([...(a ?? []), ...(b ?? [])]),
];

export function hubGroupCovered(res: HubGroupApplyResponse): CoveredSet[] {
  const byId = new Map<string, CoveredSet>();
  for (const member of res.sets) {
    for (const covered of member.covered ?? []) {
      const seen = byId.get(covered.set_id);
      byId.set(
        covered.set_id,
        seen
          ? {
              ...seen,
              geosite: union(seen.geosite, covered.geosite),
              geoip: union(seen.geoip, covered.geoip),
              asns: union(seen.asns, covered.asns),
              idle: seen.idle || covered.idle,
            }
          : covered,
      );
    }
  }
  return [...byId.values()];
}

export function hubGroupMoved(res: HubGroupApplyResponse): DomainReassignment[] {
  return res.sets.flatMap((member) => member.moved ?? []);
}

export function hubGroupWarnings(
  res: HubGroupApplyResponse,
): { name: string; warning: HubWarning }[] {
  return res.sets.flatMap((member) =>
    (member.warnings ?? []).map((warning) => ({ name: member.name, warning })),
  );
}

export interface HubGroupMemberRef {
  member: number;
  name: string;
  setId: string;
}

export function hubGroupMemberOf(body: unknown): HubGroupMemberRef | null {
  if (!body || typeof body !== "object") return null;
  const b = body as { member?: unknown; set_name?: unknown; set_id?: unknown };
  if (typeof b.member !== "number") return null;
  return {
    member: b.member,
    name: typeof b.set_name === "string" ? b.set_name : "",
    setId: typeof b.set_id === "string" ? b.set_id : "",
  };
}

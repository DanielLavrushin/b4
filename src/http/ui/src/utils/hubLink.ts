import type { B4HubOrigin } from "@models/config";
import type { HubWarning } from "@models/hub";

export interface HubLinkMerge {
  hub?: B4HubOrigin;
  warnings: HubWarning[];
}

function readLink(raw: unknown): B4HubOrigin | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const r = raw as Record<string, unknown>;
  const link: B4HubOrigin = {};
  if (typeof r.id === "string" && r.id) link.id = r.id;
  if (typeof r.version === "number" && r.version > 0) link.version = r.version;
  if (typeof r.hash === "string" && r.hash) link.hash = r.hash;
  if (typeof r.applied_at === "string" && r.applied_at) link.applied_at = r.applied_at;
  return link.id || link.hash ? link : undefined;
}

export function mergeHubLink(
  current: B4HubOrigin | undefined,
  pastedRaw: unknown,
  keepPastedAppliedAt = false,
): HubLinkMerge {
  const pasted = readLink(pastedRaw);
  if (current?.id) {
    if (!pasted?.id) return { hub: current, warnings: [] };
    if (pasted.id !== current.id) {
      return {
        hub: current,
        warnings: [{ code: "foreign_hub_link", params: { pasted: pasted.id, kept: current.id } }],
      };
    }
    if ((pasted.version ?? 0) > (current.version ?? 0) && pasted.hash) {
      const { vote: _vote, voted_at: _votedAt, ...rest } = current;
      return { hub: { ...rest, version: pasted.version, hash: pasted.hash }, warnings: [] };
    }
    return { hub: current, warnings: [] };
  }
  if (!pasted) return { hub: current, warnings: [] };
  const adopted: B4HubOrigin = {};
  if (pasted.id) {
    adopted.id = pasted.id;
    if (pasted.version) adopted.version = pasted.version;
  }
  if (pasted.hash) adopted.hash = pasted.hash;
  if (keepPastedAppliedAt && pasted.applied_at) adopted.applied_at = pasted.applied_at;
  return { hub: adopted, warnings: [] };
}

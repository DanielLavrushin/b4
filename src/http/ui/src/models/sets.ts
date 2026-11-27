import type { B4SetConfig } from "./config";

export function duplicateSetDraft(set: B4SetConfig): Omit<B4SetConfig, "id"> {
  const { id: _, revision: _revision, ...rest } = structuredClone(set);
  if (rest.discovery) rest.discovery = { ...rest.discovery, watchdog: false };
  rest.targets = { ...rest.targets, ip_priority: false };
  if (rest.hub) {
    const { group_id: _groupId, group_version: _groupVersion, ...hub } =
      rest.hub;
    rest.hub = hub;
  }
  return { ...rest, name: `${set.name} (copy)` };
}

export interface SetDomainMatch {
  domain: string;
  set_name: string;
  set_id: string;
  via: string;
  relation: string;
  entry: string;
  enabled: boolean;
  handles?: boolean;
}

export interface DomainReassignment {
  domain: string;
  set_name: string;
  set_id: string;
}

export interface CoveredSet {
  set_id: string;
  set_name: string;
  geosite: string[];
  geoip: string[];
  asns: string[];
  idle: boolean;
}

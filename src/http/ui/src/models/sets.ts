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

export interface SetOverlapEntry {
  entry: string;
  source?: string;
}

export interface SetOverlapExample {
  domain: string;
  entries: [SetOverlapEntry, SetOverlapEntry];
  relation: string;
  winner_id?: string;
  winner_name?: string;
  reason?: "specific" | "order" | "filter" | "other";
}

export interface SetOverlap {
  set_ids: [string, string];
  set_names: [string, string];
  shared_geosite?: string[];
  shared_geoip?: string[];
  domains: number;
  examples?: SetOverlapExample[];
}

export interface SetOverlapView {
  otherId: string;
  otherName: string;
  overlap: SetOverlap;
}

export const overlapsForSet = (
  overlaps: SetOverlap[] | undefined,
  setId: string,
): SetOverlapView[] =>
  (overlaps ?? []).flatMap((overlap) => {
    const own = overlap.set_ids.indexOf(setId);
    if (own < 0) return [];
    const other = 1 - own;
    return [
      {
        otherId: overlap.set_ids[other],
        otherName: overlap.set_names[other],
        overlap,
      },
    ];
  });

export interface DomainReassignment {
  domain: string;
  set_name: string;
  set_id: string;
}

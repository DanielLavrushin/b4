export interface ConnectionGroup {
  key: string;
  mac: string;
  domain: string;
  destIp: string;
  destIps: string[];
  protocol: "TCP" | "UDP";
  tls: string;
  hostSet: string;
  ipSet: string;
  flags: string;
  packets: number;
  firstSeen: number;
  lastSeen: number;
  buckets: number[];
}

export interface DeviceSummary {
  mac: string;
  packets: number;
  groups: number;
  lastSeen: number;
  buckets: number[];
}

export interface AggregatorSnapshot {
  groups: ConnectionGroup[];
  devices: DeviceSummary[];
  totalPackets: number;
  unmatchedCount: number;
  ts: number;
}

export type AggregatorInput =
  | { type: "ingest"; lines: string[] }
  | { type: "clear" }
  | { type: "setSnapshotInterval"; ms: number }
  | { type: "setBucketCount"; count: number }
  | { type: "setIpToMac"; map: Record<string, string> }
  | { type: "flush" };

export type AggregatorOutput = { type: "snapshot"; payload: AggregatorSnapshot };

export const BUCKET_SIZE_MS = 1000;
export const DEFAULT_BUCKETS = 60;
export const MAX_GROUPS = 2000;

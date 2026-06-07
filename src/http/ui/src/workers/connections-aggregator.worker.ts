/// <reference lib="webworker" />
import {
  AggregatorInput,
  AggregatorOutput,
  AggregatorSnapshot,
  ConnectionGroup,
  DeviceSummary,
  BUCKET_SIZE_MS,
  DEFAULT_BUCKETS,
  MAX_GROUPS,
} from "./connections-types";

const ctx: DedicatedWorkerGlobalScope =
  globalThis as unknown as DedicatedWorkerGlobalScope;

let bucketCount = DEFAULT_BUCKETS;
let snapshotInterval = 500;

const groups = new Map<string, ConnectionGroup>();
const devices = new Map<string, DeviceSummary>();
let ipToMac: Record<string, string> = {};
const learnedIpToMac = new Map<string, string>();
let totalPackets = 0;
let unmatchedCount = 0;
let latestTs = 0;
let dirty = false;
let snapshotTimer: ReturnType<typeof setInterval> | null = null;

function parseLine(line: string): {
  timestamp: number;
  protocol: "TCP" | "UDP";
  hostSet: string;
  domain: string;
  source: string;
  ipSet: string;
  destination: string;
  sourceAlias: string;
  tls: string;
  flags: string;
} | null {
  const tokens = line.trim().split(",");
  if (tokens.length < 7) return null;
  const [
    rawTs,
    protocol,
    hostSet,
    domain,
    source,
    ipSet,
    destination,
    sourceAlias,
    tls,
    flags,
  ] = tokens;
  const cleanTs = rawTs.replaceAll(" [INFO]", "").trim().split(".")[0];
  const ts = Date.parse(cleanTs.replaceAll("/", "-")) || latestTs;
  if (protocol !== "TCP" && protocol !== "UDP") return null;
  return {
    timestamp: ts,
    protocol,
    hostSet: hostSet || "",
    domain: domain || "",
    source: source || "",
    ipSet: ipSet || "",
    destination: destination || "",
    sourceAlias: sourceAlias || "",
    tls: tls || "",
    flags: flags || "",
  };
}

function normalizeMac(mac: string): string {
  return mac.toUpperCase().replaceAll("-", ":");
}

function stripPort(addr: string): string {
  if (!addr) return addr;
  if (addr.startsWith("[")) {
    const end = addr.indexOf("]");
    return end > 0 ? addr.slice(1, end) : addr.slice(1);
  }
  const firstColon = addr.indexOf(":");
  if (firstColon !== -1 && firstColon === addr.lastIndexOf(":"))
    return addr.slice(0, firstColon);
  return addr;
}

function bucketIndex(ts: number, now: number): number {
  const offset = Math.floor((now - ts) / BUCKET_SIZE_MS);
  if (offset < 0 || offset >= bucketCount) return -1;
  return bucketCount - 1 - offset;
}

function rotateBuckets(
  g: { buckets: number[]; lastSeen: number },
  now: number,
): void {
  const shift = Math.floor((now - g.lastSeen) / BUCKET_SIZE_MS);
  if (shift <= 0) return;
  if (shift >= bucketCount) {
    for (let i = 0; i < bucketCount; i++) g.buckets[i] = 0;
    return;
  }
  for (let i = 0; i < bucketCount - shift; i++)
    g.buckets[i] = g.buckets[i + shift];
  for (let i = bucketCount - shift; i < bucketCount; i++) g.buckets[i] = 0;
}

function evictIfNeeded(): void {
  if (groups.size <= MAX_GROUPS) return;
  const entries = Array.from(groups.entries()).sort(
    (a, b) => a[1].lastSeen - b[1].lastSeen,
  );
  const toRemove = groups.size - MAX_GROUPS;
  for (let i = 0; i < toRemove; i++) groups.delete(entries[i][0]);
}

function mergeSeries(
  target: { buckets: number[]; packets: number; lastSeen: number },
  source: { buckets: number[]; packets: number; lastSeen: number },
  now: number,
): void {
  rotateBuckets(target, now);
  rotateBuckets(source, now);
  for (let i = 0; i < bucketCount; i++) target.buckets[i] += source.buckets[i];
  target.packets += source.packets;
  target.lastSeen = Math.max(target.lastSeen, source.lastSeen);
}

function reassignIpToMac(ip: string, mac: string, now: number): void {
  const srcDev = devices.get(ip);
  if (srcDev) {
    const tgtDev = devices.get(mac);
    if (tgtDev) {
      mergeSeries(tgtDev, srcDev, now);
    } else {
      srcDev.mac = mac;
      devices.set(mac, srcDev);
    }
    devices.delete(ip);
  }

  for (const [key, g] of Array.from(groups)) {
    if (g.mac !== ip) continue;
    const newKey = mac + key.slice(ip.length);
    const tgt = groups.get(newKey);
    if (tgt) {
      mergeSeries(tgt, g, now);
      if (g.firstSeen < tgt.firstSeen) tgt.firstSeen = g.firstSeen;
      for (const dip of g.destIps)
        if (!tgt.destIps.includes(dip)) tgt.destIps.push(dip);
      if (!tgt.tls && g.tls) tgt.tls = g.tls;
      if (!tgt.hostSet && g.hostSet) tgt.hostSet = g.hostSet;
      if (!tgt.ipSet && g.ipSet) tgt.ipSet = g.ipSet;
      if (!tgt.flags && g.flags) tgt.flags = g.flags;
      groups.delete(key);
    } else {
      g.mac = mac;
      g.key = newKey;
      groups.delete(key);
      groups.set(newKey, g);
    }
  }
}

function resolveSourceMac(
  p: { sourceAlias: string; source: string },
  now: number,
): string {
  const ip = stripPort(p.source);
  const mac = p.sourceAlias ? normalizeMac(p.sourceAlias) : "";
  if (mac) {
    if (ip && learnedIpToMac.get(ip) !== mac) {
      learnedIpToMac.set(ip, mac);
      if (ip !== mac && devices.has(ip)) reassignIpToMac(ip, mac, now);
    }
    return mac;
  }
  return ipToMac[ip] || learnedIpToMac.get(ip) || ip;
}

function ingest(lines: string[]): void {
  for (const line of lines) {
    const p = parseLine(line);
    if (!p) continue;

    if (p.timestamp > latestTs) latestTs = p.timestamp;
    const now = latestTs;

    const mac = resolveSourceMac(p, now);
    const groupIdent = p.domain || p.destination || "?";
    const key = `${mac}|${p.protocol}|${groupIdent}`;

    let g = groups.get(key);
    if (!g) {
      g = {
        key,
        mac,
        domain: p.domain,
        destIp: p.destination,
        destIps: p.destination ? [p.destination] : [],
        protocol: p.protocol,
        tls: p.tls,
        hostSet: p.hostSet,
        ipSet: p.ipSet,
        flags: p.flags,
        packets: 0,
        firstSeen: p.timestamp,
        lastSeen: p.timestamp,
        buckets: new Array<number>(bucketCount).fill(0),
      };
      groups.set(key, g);
    }

    rotateBuckets(g, now);
    g.lastSeen = Math.max(g.lastSeen, p.timestamp);
    g.packets += 1;
    if (p.destination && !g.destIps.includes(p.destination)) {
      g.destIps.push(p.destination);
      g.destIp = p.destination;
    }
    if (p.tls && p.tls !== g.tls) g.tls = p.tls;
    if (p.hostSet && p.hostSet !== g.hostSet) g.hostSet = p.hostSet;
    if (p.ipSet && p.ipSet !== g.ipSet) g.ipSet = p.ipSet;
    if (p.flags && p.flags !== g.flags) g.flags = p.flags;

    const idx = bucketIndex(p.timestamp, now);
    if (idx >= 0) g.buckets[idx] += 1;

    let d = devices.get(mac);
    if (!d) {
      d = {
        mac,
        packets: 0,
        groups: 0,
        lastSeen: p.timestamp,
        buckets: new Array<number>(bucketCount).fill(0),
      };
      devices.set(mac, d);
    }
    rotateBuckets(d, now);
    d.lastSeen = Math.max(d.lastSeen, p.timestamp);
    d.packets += 1;
    if (idx >= 0) d.buckets[idx] += 1;

    totalPackets += 1;
    if (!p.hostSet && !p.ipSet) unmatchedCount += 1;
  }

  for (const d of devices.values()) {
    d.groups = 0;
  }
  for (const g of groups.values()) {
    const d = devices.get(g.mac);
    if (d) d.groups += 1;
  }

  evictIfNeeded();
  dirty = true;
}

function buildSnapshot(): AggregatorSnapshot {
  const now = latestTs;
  const arrGroups: ConnectionGroup[] = [];
  for (const g of groups.values()) {
    rotateBuckets(g, now);
    arrGroups.push({ ...g, destIps: [...g.destIps], buckets: [...g.buckets] });
  }
  const arrDevices: DeviceSummary[] = [];
  for (const d of devices.values()) {
    rotateBuckets(d, now);
    arrDevices.push({ ...d, buckets: [...d.buckets] });
  }
  return {
    groups: arrGroups,
    devices: arrDevices,
    totalPackets,
    unmatchedCount,
    ts: now,
  };
}

function postSnapshot(): void {
  if (!dirty) return;
  const msg: AggregatorOutput = { type: "snapshot", payload: buildSnapshot() };
  ctx.postMessage(msg);
  dirty = false;
}

function startTimer(): void {
  if (snapshotTimer) clearInterval(snapshotTimer);
  snapshotTimer = setInterval(postSnapshot, snapshotInterval);
}

function clearState(): void {
  groups.clear();
  devices.clear();
  learnedIpToMac.clear();
  totalPackets = 0;
  unmatchedCount = 0;
  latestTs = 0;
  dirty = true;
}

ctx.onmessage = (e: MessageEvent<AggregatorInput>) => {
  const msg = e.data;
  switch (msg.type) {
    case "ingest":
      ingest(msg.lines);
      break;
    case "clear":
      clearState();
      break;
    case "setSnapshotInterval":
      snapshotInterval = Math.max(50, msg.ms);
      startTimer();
      break;
    case "setBucketCount":
      bucketCount = Math.max(10, Math.min(300, msg.count));
      clearState();
      break;
    case "setIpToMac":
      ipToMac = msg.map;
      break;
    case "flush":
      dirty = true;
      postSnapshot();
      break;
  }
};

startTimer();

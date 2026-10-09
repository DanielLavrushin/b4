import {
  MINUTE_BUCKETS,
  RSS_BUCKETS,
  TEN_MINUTE_BUCKETS,
} from "../models/metrics";
import type {
  ActivityBucket,
  Attention,
  B4Event,
  BlockedEntry,
  BlockedLists,
  EngineInfo,
  EngineMode,
  EngineState,
  EscalationEntry,
  EscalationList,
  EventCode,
  EventLevel,
  EventLog,
  MetricsFrame,
  MTProtoSecretStat,
  MTProtoStats,
  ProcessInfo,
  RSSBucket,
  RulesInfo,
  SetActivity,
  SetKind,
  TopEntry,
  TopList,
  Totals,
  UpstreamAttention,
} from "../models/metrics";
import type { EngineFailure } from "../models/settings";

export const CLOCK_JUMP_MS = 2000;
export const BUCKET_MATCH_MS = 5000;

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj =>
  typeof v === "object" && v !== null && !Array.isArray(v);
const num = (v: unknown): number =>
  typeof v === "number" && Number.isFinite(v) ? v : 0;
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const bool = (v: unknown): boolean => v === true;
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const objects = (v: unknown): Obj[] => list(v).filter(isObj);

function sortedByT<T extends { t: number }>(items: T[]): T[] {
  for (let i = 1; i < items.length; i++) {
    if (items[i].t < items[i - 1].t) return items.slice().sort((a, b) => a.t - b.t);
  }
  return items;
}

function activityBuckets(v: unknown): ActivityBucket[] {
  const out: ActivityBucket[] = [];
  for (const o of objects(v)) {
    const t = num(o.t);
    if (t > 0) out.push({ t, in_sets: num(o.in_sets), not_in_set: num(o.not_in_set) });
  }
  return sortedByT(out);
}

function rssBuckets(v: unknown): RSSBucket[] {
  const out: RSSBucket[] = [];
  for (const o of objects(v)) {
    const t = num(o.t);
    if (t > 0) out.push({ t, max: num(o.max) });
  }
  return sortedByT(out);
}

function engineInfo(v: unknown): EngineInfo {
  const o = isObj(v) ? v : {};
  return {
    state: str(o.state) as EngineState,
    mode: str(o.mode) as EngineMode,
    threads: num(o.threads),
    firewall: str(o.firewall),
  };
}

function rulesInfo(v: unknown): RulesInfo {
  const o = isObj(v) ? v : {};
  return {
    monitored: bool(o.monitored),
    interval_s: num(o.interval_s),
    last_check: num(o.last_check),
    restores: num(o.restores),
    last_restore: num(o.last_restore),
  };
}

function processInfo(v: unknown): ProcessInfo {
  const o = isObj(v) ? v : {};
  return {
    rss_bytes: num(o.rss_bytes),
    mem_total_bytes: num(o.mem_total_bytes),
    cpu_percent: num(o.cpu_percent),
    cpu_peak_percent: num(o.cpu_peak_percent),
    cpu_peak_at: num(o.cpu_peak_at),
    cpu_cores: num(o.cpu_cores),
    os_threads: num(o.os_threads),
    thread_warn: num(o.thread_warn),
    thread_limit: num(o.thread_limit),
  };
}

function setActivity(o: Obj): SetActivity {
  const out: SetActivity = {
    id: str(o.id),
    name: str(o.name),
    kind: str(o.kind) as SetKind,
    watched: bool(o.watched),
    conns_60m: num(o.conns_60m),
    last_match: num(o.last_match),
    dns_blocked: num(o.dns_blocked),
  };
  if (typeof o.open === "number" && Number.isFinite(o.open)) out.open = o.open;
  return out;
}

function totals(v: unknown): Totals {
  const o = isObj(v) ? v : {};
  return {
    conns: num(o.conns),
    conns_in_sets: num(o.conns_in_sets),
    rst_dropped: num(o.rst_dropped),
    escalations: num(o.escalations),
    blocked_dns: num(o.blocked_dns),
    blocked_conns: num(o.blocked_conns),
  };
}

function blockedEntries(v: unknown): BlockedEntry[] {
  return objects(v).map((o) => ({ key: str(o.key), count: num(o.count), last: num(o.last) }));
}

function blockedLists(v: unknown): BlockedLists | undefined {
  if (!isObj(v)) return undefined;
  return { rev: num(v.rev), domains: blockedEntries(v.domains), devices: blockedEntries(v.devices) };
}

function topEntry(o: Obj): TopEntry {
  return {
    key: str(o.key),
    count: num(o.count),
    last: num(o.last),
    sets: list(o.sets).filter((s): s is string => typeof s === "string" && s !== ""),
  };
}

function topList(v: unknown): TopList | undefined {
  if (!isObj(v)) return undefined;
  return { rev: num(v.rev), items: objects(v.items).map(topEntry) };
}

function escalationList(v: unknown): EscalationList | undefined {
  if (!isObj(v)) return undefined;
  const items: EscalationEntry[] = objects(v.items).map((o) => ({
    host: str(o.host),
    to_set: str(o.to_set),
    hops: num(o.hops),
    set_at: str(o.set_at),
    expires_at: str(o.expires_at),
  }));
  return { rev: num(v.rev), items };
}

function event(o: Obj): B4Event {
  const out: B4Event = {
    id: num(o.id),
    t: num(o.t),
    level: str(o.level) as EventLevel,
    code: str(o.code) as EventCode,
    message: str(o.message),
  };
  if (isObj(o.args)) {
    const args: Record<string, string> = {};
    for (const [key, value] of Object.entries(o.args)) {
      if (typeof value === "string") args[key] = value;
    }
    out.args = args;
  }
  return out;
}

function eventLog(v: unknown): EventLog | undefined {
  if (!isObj(v)) return undefined;
  return { rev: num(v.rev), items: objects(v.items).map(event), errors: objects(v.errors).map(event) };
}

function upstream(o: Obj): UpstreamAttention {
  return {
    set_id: str(o.set_id),
    set_name: str(o.set_name),
    upstream: str(o.upstream),
    fail_open: bool(o.fail_open),
    failures: num(o.failures),
    last_error: str(o.last_error),
    last_failure: num(o.last_failure),
  };
}

function attention(v: unknown): Attention {
  const o = isObj(v) ? v : {};
  const overload = isObj(o.overload) ? o.overload : {};
  const out: Attention = {
    upstreams: objects(o.upstreams).map(upstream),
    binary_replaced: bool(o.binary_replaced),
    overload: {
      window_s: num(overload.window_s),
      inject_skipped: num(overload.inject_skipped),
      raw_send_dropped: num(overload.raw_send_dropped),
      queue_overflow: num(overload.queue_overflow),
    },
  };
  if (isObj(o.conntrack)) out.conntrack = { count: num(o.conntrack.count), max: num(o.conntrack.max) };
  return out;
}

function secret(o: Obj): MTProtoSecretStat {
  return {
    name: str(o.name),
    active: num(o.active),
    total: num(o.total),
    bytes_up: num(o.bytes_up),
    bytes_down: num(o.bytes_down),
    networks: num(o.networks),
    network_addrs: list(o.network_addrs).filter((a): a is string => typeof a === "string"),
  };
}

function mtproto(v: unknown): MTProtoStats | undefined {
  if (!isObj(v)) return undefined;
  return {
    enabled: bool(v.enabled),
    port: num(v.port),
    networks: num(v.networks),
    active_connections: num(v.active_connections),
    total_connections: num(v.total_connections),
    bytes_up: num(v.bytes_up),
    bytes_down: num(v.bytes_down),
    secrets: objects(v.secrets).map(secret),
  };
}

function engineFailure(v: unknown): EngineFailure | undefined {
  if (!isObj(v)) return undefined;
  return {
    mode: str(v.mode) as EngineFailure["mode"],
    error: str(v.error),
    retry_at: num(v.retry_at),
    retries_left: num(v.retries_left),
  };
}

export function normalizeFrame(raw: unknown): MetricsFrame | null {
  if (!isObj(raw)) return null;
  if (raw.type !== "hello" && raw.type !== "tick") return null;
  const now = num(raw.now);
  if (!(now > 0)) return null;
  const activity = isObj(raw.activity) ? raw.activity : {};
  const frame: MetricsFrame = {
    type: raw.type,
    now,
    uptime_s: num(raw.uptime_s),
    stats_since: num(raw.stats_since),
    engine: engineInfo(raw.engine),
    rules: rulesInfo(raw.rules),
    last_packet_at: num(raw.last_packet_at),
    process: processInfo(raw.process),
    activity: {
      minute: activityBuckets(activity.minute),
      ten_minute: activityBuckets(activity.ten_minute),
    },
    rss_history: rssBuckets(raw.rss_history),
    sets: objects(raw.sets).map(setActivity),
    sets_disabled: num(raw.sets_disabled),
    totals: totals(raw.totals),
    attention: attention(raw.attention),
  };
  const blocked = blockedLists(raw.blocked);
  if (blocked) frame.blocked = blocked;
  const domains = topList(raw.top_domains);
  if (domains) frame.top_domains = domains;
  const addresses = topList(raw.top_addresses);
  if (addresses) frame.top_addresses = addresses;
  const escalations = escalationList(raw.escalations);
  if (escalations) frame.escalations = escalations;
  const events = eventLog(raw.events);
  if (events) frame.events = events;
  const mt = mtproto(raw.mtproto);
  if (mt) frame.mtproto = mt;
  const failure = engineFailure(raw.engine_failure);
  if (failure) frame.engine_failure = failure;
  return frame;
}

export function parseFrame(data: unknown): MetricsFrame | null {
  if (typeof data !== "string") return null;
  try {
    return normalizeFrame(JSON.parse(data));
  } catch {
    return null;
  }
}

function flatEqual(a: object, b: object): boolean {
  const ka = Object.keys(a);
  if (ka.length !== Object.keys(b).length) return false;
  for (const key of ka) {
    if (!Object.is((a as Obj)[key], (b as Obj)[key])) return false;
  }
  return true;
}

export function mergeBuckets<T extends { t: number }>(
  prev: readonly T[],
  incoming: readonly T[],
  cap: number,
): T[] {
  if (incoming.length === 0) return prev.length > cap ? prev.slice(prev.length - cap) : prev.slice();
  const out = prev.slice();
  const used: boolean[] = new Array<boolean>(out.length).fill(false);
  for (const bucket of incoming) {
    let best = -1;
    let bestDistance = Number.POSITIVE_INFINITY;
    for (let i = out.length - 1; i >= 0; i--) {
      if (used[i]) continue;
      const distance = Math.abs(out[i].t - bucket.t);
      if (distance <= BUCKET_MATCH_MS && distance < bestDistance) {
        best = i;
        bestDistance = distance;
      }
      if (out[i].t < bucket.t - BUCKET_MATCH_MS) break;
    }
    if (best >= 0) {
      if (!flatEqual(out[best], bucket)) out[best] = bucket;
      used[best] = true;
      continue;
    }
    let at = out.length;
    while (at > 0 && out[at - 1].t > bucket.t) at--;
    out.splice(at, 0, bucket);
    used.splice(at, 0, true);
  }
  return out.length > cap ? out.slice(out.length - cap) : out;
}

export function clockShift(prev: MetricsFrame, next: MetricsFrame): number {
  const drift = next.now - prev.now - (next.uptime_s - prev.uptime_s) * 1000;
  return Math.abs(drift) > CLOCK_JUMP_MS ? drift : 0;
}

export function isRestart(prev: MetricsFrame, next: MetricsFrame): boolean {
  return next.uptime_s < prev.uptime_s - 1;
}

const shiftTime = (t: number, delta: number): number => (t > 0 ? t + delta : t);

function shiftBuckets<T extends { t: number }>(items: readonly T[], delta: number): T[] {
  return items.map((b) => ({ ...b, t: b.t + delta }));
}

function shiftEvents(items: readonly B4Event[], delta: number): B4Event[] {
  return items.map((e) => ({ ...e, t: shiftTime(e.t, delta) }));
}

function shiftEntries<T extends { last: number }>(items: readonly T[], delta: number): T[] {
  return items.map((e) => ({ ...e, last: shiftTime(e.last, delta) }));
}

export function relabelFrame(frame: MetricsFrame, delta: number): MetricsFrame {
  if (delta === 0) return frame;
  const out: MetricsFrame = {
    ...frame,
    activity: {
      minute: shiftBuckets(frame.activity.minute, delta),
      ten_minute: shiftBuckets(frame.activity.ten_minute, delta),
    },
    rss_history: shiftBuckets(frame.rss_history, delta),
  };
  if (frame.blocked) {
    out.blocked = {
      ...frame.blocked,
      domains: shiftEntries(frame.blocked.domains, delta),
      devices: shiftEntries(frame.blocked.devices, delta),
    };
  }
  if (frame.top_domains) {
    out.top_domains = {
      ...frame.top_domains,
      items: shiftEntries(frame.top_domains.items, delta),
    };
  }
  if (frame.top_addresses) {
    out.top_addresses = {
      ...frame.top_addresses,
      items: shiftEntries(frame.top_addresses.items, delta),
    };
  }
  if (frame.events) {
    out.events = {
      ...frame.events,
      items: shiftEvents(frame.events.items, delta),
      errors: shiftEvents(frame.events.errors, delta),
    };
  }
  return out;
}

function newerList<T extends { rev: number }>(prev: T | undefined, next: T | undefined): T | undefined {
  if (!next) return prev;
  if (!prev || next.rev > prev.rev) return next;
  return prev;
}

export function mergeFrame(prev: MetricsFrame | null, next: MetricsFrame): MetricsFrame {
  if (!prev) return next;
  if (next.type === "hello" || isRestart(prev, next)) return shareEqual(prev, next);
  const base = relabelFrame(prev, clockShift(prev, next));
  const merged: MetricsFrame = {
    ...next,
    activity: {
      minute: mergeBuckets(base.activity.minute, next.activity.minute, MINUTE_BUCKETS),
      ten_minute: mergeBuckets(base.activity.ten_minute, next.activity.ten_minute, TEN_MINUTE_BUCKETS),
    },
    rss_history: mergeBuckets(base.rss_history, next.rss_history, RSS_BUCKETS),
  };
  const blocked = newerList(base.blocked, next.blocked);
  if (blocked) merged.blocked = blocked;
  else delete merged.blocked;
  const domains = newerList(base.top_domains, next.top_domains);
  if (domains) merged.top_domains = domains;
  else delete merged.top_domains;
  const addresses = newerList(base.top_addresses, next.top_addresses);
  if (addresses) merged.top_addresses = addresses;
  else delete merged.top_addresses;
  const escalations = newerList(base.escalations, next.escalations);
  if (escalations) merged.escalations = escalations;
  else delete merged.escalations;
  const events = newerList(base.events, next.events);
  if (events) merged.events = events;
  else delete merged.events;
  return shareEqual(prev, merged);
}

const isPlain = (v: unknown): v is Obj => {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return false;
  const proto: unknown = Object.getPrototypeOf(v);
  return proto === Object.prototype || proto === null;
};

function identityOf(v: unknown): string | undefined {
  if (!isPlain(v)) return undefined;
  if (typeof v.id === "number" || typeof v.id === "string") return `i${String(v.id)}`;
  if (typeof v.key === "string") return `k${v.key}`;
  if (typeof v.t === "number") return `t${v.t}`;
  return undefined;
}

function shareArray(prev: readonly unknown[], next: readonly unknown[]): readonly unknown[] {
  let byIdentity: Map<string, unknown> | null = null;
  if (prev.length > 0 && identityOf(prev[0]) !== undefined) {
    byIdentity = new Map();
    for (const item of prev) {
      const id = identityOf(item);
      if (id !== undefined) byIdentity.set(id, item);
    }
  }
  let same = prev.length === next.length;
  const out: unknown[] = new Array<unknown>(next.length);
  for (let i = 0; i < next.length; i++) {
    const item = next[i];
    let candidate: unknown = i < prev.length ? prev[i] : undefined;
    if (byIdentity) {
      const id = identityOf(item);
      candidate = id === undefined ? undefined : byIdentity.get(id);
    }
    const value = candidate === undefined ? item : shareEqual(candidate, item);
    out[i] = value;
    if (same && value !== prev[i]) same = false;
  }
  return same ? prev : out;
}

function shareObject(prev: Obj, next: Obj): Obj {
  const keys = Object.keys(next);
  let same = keys.length === Object.keys(prev).length;
  const out: Obj = {};
  for (const key of keys) {
    const has = Object.prototype.hasOwnProperty.call(prev, key);
    const value = has ? shareEqual(prev[key], next[key]) : next[key];
    out[key] = value;
    if (same && (!has || value !== prev[key])) same = false;
  }
  return same ? prev : out;
}

export function shareEqual<T>(prev: T, next: T): T {
  if (Object.is(prev, next)) return prev;
  if (Array.isArray(prev) && Array.isArray(next)) return shareArray(prev, next) as T;
  if (isPlain(prev) && isPlain(next)) return shareObject(prev, next) as T;
  return next;
}

import { B4SetConfig } from "@models/config";
import {
  DiscoveryOutcome,
  DiscoveryResult,
  DiscoverySuite,
  HistoryEntry,
  StrategyFamily,
} from "@models/discovery";

export type SiteVerdict = DiscoveryOutcome | "checking";

export const NO_BYPASS_PRESET = "no-bypass";

type TFn = (key: string, opts?: Record<string, unknown>) => string;

interface VerdictSource {
  outcome?: DiscoveryOutcome;
  best_success?: boolean;
  best_preset?: string;
  baseline_works?: boolean;
  dns_result?: { transport_blocked?: boolean; alternative_ips?: string[] };
}

export function verdictOf(r: VerdictSource, finished: boolean): SiteVerdict {
  if (r.outcome) return r.outcome;
  if (r.baseline_works) return "works_without_bypass";
  if (r.best_success && r.best_preset && r.best_preset !== NO_BYPASS_PRESET) {
    return "found";
  }
  if (r.dns_result?.transport_blocked && !r.dns_result.alternative_ips?.length) {
    return "address_blocked";
  }
  return finished ? "not_found" : "checking";
}

export interface Confirmation {
  passed: number;
  tries: number;
}

export function confirmationOf(
  dr: DiscoveryResult,
  preset: string,
): Confirmation | null {
  const r = dr.results?.[preset];
  const tries = r?.confirm_tries ?? dr.confirm_tries ?? 0;
  if (!tries) return null;
  return { passed: r?.confirmed ?? dr.confirmed ?? 0, tries };
}

export interface ApplyTarget {
  domains: string[];
  set: B4SetConfig;
  preset: string;
}

export interface FoundGroup {
  kind: "found";
  key: string;
  domains: string[];
  preset: string;
  family?: StrategyFamily;
  set: B4SetConfig | null;
  results: DiscoveryResult[];
  confirmation: Confirmation | null;
  unconfirmed: boolean;
  order: number;
}

export interface SiteEntry {
  kind: "site";
  key: string;
  domain: string;
  verdict: Exclude<SiteVerdict, "found">;
  result: DiscoveryResult;
  order: number;
}

export type ResultEntry = FoundGroup | SiteEntry;

const emptyDns = () => ({
  enabled: false,
  target_dns: "",
  doh_url: "",
  fragment_query: false,
  strict: false,
});

export function pinsFor(
  set: B4SetConfig,
  domains: string[],
): Record<string, string[]> | undefined {
  const all = set.dns?.pins;
  if (!all) return undefined;
  const wanted = domains.map((d) => d.toLowerCase());
  const kept: Record<string, string[]> = {};
  for (const [pin, ips] of Object.entries(all)) {
    const key = pin.toLowerCase();
    if (wanted.some((d) => d === key || d.endsWith(`.${key}`))) kept[key] = ips;
  }
  return Object.keys(kept).length > 0 ? kept : undefined;
}

export function normalizeSet(set: B4SetConfig): B4SetConfig {
  const targets = set.targets ?? ({} as B4SetConfig["targets"]);
  return {
    ...set,
    targets: {
      ...targets,
      sni_domains: targets.sni_domains ?? [],
      ip: targets.ip ?? [],
      geosite_categories: targets.geosite_categories ?? [],
      geoip_categories: targets.geoip_categories ?? [],
    },
    dns: set.dns ?? emptyDns(),
  };
}

export function scopeSet(
  set: B4SetConfig,
  domains: string[],
  keepDns: boolean,
): B4SetConfig {
  const base = normalizeSet(set);
  const pins = pinsFor(base, domains);
  return {
    ...base,
    targets: {
      ...base.targets,
      sni_domains: [...domains],
      ip: [],
      geosite_categories: [],
      geoip_categories: [],
    },
    dns: { ...(keepDns ? base.dns : emptyDns()), pins },
  };
}

export const hasPins = (set: B4SetConfig): boolean =>
  Object.keys(set.dns?.pins ?? {}).length > 0;

export function dnsPoisoned(results: DiscoveryResult[]): boolean {
  return results.some((dr) => !!dr.dns_result?.is_poisoned);
}

export function buildResultEntries(
  suite: DiscoverySuite,
  finished: boolean,
): ResultEntry[] {
  const results = suite.domain_discovery_results ?? {};
  const order = new Map<string, number>();
  (suite.domains ?? []).forEach((d, i) => order.set(d.domain, i));
  Object.keys(results).forEach((d) => {
    if (!order.has(d)) order.set(d, order.size);
  });
  const rank = (d: string) => order.get(d) ?? Number.MAX_SAFE_INTEGER;
  const byRank = (a: string, b: string) => rank(a) - rank(b);

  const entries: ResultEntry[] = [];
  const grouped = new Set<string>();

  const pushGroup = (
    domains: string[],
    preset: string,
    set: B4SetConfig | null,
    family?: StrategyFamily,
  ) => {
    const rs = domains.map((d) => results[d]).filter(Boolean);
    const confs = rs.map((dr) => confirmationOf(dr, preset));
    const confirmation =
      confs.length > 0 && confs.every((c) => c !== null)
        ? {
            passed: Math.min(...confs.map((c) => c.passed)),
            tries: confs[0].tries,
          }
        : null;
    const unconfirmed = rs.some((dr, i) => {
      if (dr.unconfirmed !== undefined) return dr.unconfirmed;
      const c = confs[i];
      return !c || c.passed < c.tries;
    });
    entries.push({
      kind: "found",
      key: `${preset}::${domains.join(",")}`,
      domains,
      preset,
      family: family ?? rs[0]?.results?.[preset]?.family,
      set,
      results: rs,
      confirmation,
      unconfirmed,
      order: Math.min(...domains.map(rank)),
    });
    domains.forEach((d) => grouped.add(d));
  };

  for (const g of suite.strategy_groups ?? []) {
    const domains = g.domains
      .filter((d) => results[d] && verdictOf(results[d], finished) === "found")
      .sort(byRank);
    if (domains.length === 0) continue;
    pushGroup(domains, g.winner_preset, g.set ? normalizeSet(g.set) : null, g.family);
  }

  const fallback = new Map<string, string[]>();
  for (const dr of Object.values(results)) {
    if (grouped.has(dr.domain)) continue;
    const verdict = verdictOf(dr, finished);
    if (verdict === "found") {
      const list = fallback.get(dr.best_preset) ?? [];
      list.push(dr.domain);
      fallback.set(dr.best_preset, list);
      continue;
    }
    entries.push({
      kind: "site",
      key: dr.domain,
      domain: dr.domain,
      verdict,
      result: dr,
      order: rank(dr.domain),
    });
  }
  for (const [preset, domains] of fallback) {
    const sorted = [...domains].sort(byRank);
    const rs = sorted.map((d) => results[d]);
    const raw = rs[0]?.results?.[preset]?.set;
    pushGroup(sorted, preset, raw ? scopeSet(raw, sorted, dnsPoisoned(rs)) : null);
  }

  return entries.sort((a, b) => a.order - b.order);
}

export interface Alternate {
  preset: string;
  family?: StrategyFamily;
  set: B4SetConfig;
  speed: number;
}

export function alternatesFor(group: FoundGroup, limit = 5): Alternate[] {
  const first = group.results[0];
  if (!first) return [];
  const keepDns = dnsPoisoned(group.results);
  const out: Alternate[] = [];
  for (const [preset, r] of Object.entries(first.results)) {
    if (preset === group.preset || preset === NO_BYPASS_PRESET) continue;
    if (r.status !== "complete" || !r.set) continue;
    if (!group.results.every((dr) => dr.results[preset]?.status === "complete")) {
      continue;
    }
    out.push({
      preset,
      family: r.family,
      set: scopeSet(r.set, group.domains, keepDns),
      speed: Math.min(...group.results.map((dr) => dr.results[preset]?.speed ?? 0)),
    });
  }
  out.sort((a, b) => b.speed - a.speed);
  return limit > 0 ? out.slice(0, limit) : out;
}

export interface TestedCounts {
  tested: number;
  worked: number;
  failed: number;
}

export function testedCounts(dr: DiscoveryResult): TestedCounts {
  const all = Object.values(dr.results ?? {});
  const worked = all.filter((r) => r.status === "complete").length;
  const failed = all.filter((r) => r.status === "failed").length;
  return { tested: all.length, worked, failed };
}

export function triesUntilFound(dr: DiscoveryResult, preset: string): number {
  const winner = dr.results?.[preset];
  if (!winner) return 0;
  const priority = winner.priority ?? 0;
  const before = Object.values(dr.results).filter(
    (r) =>
      r.preset_name !== preset &&
      (r.phase ?? "") === (winner.phase ?? "") &&
      (r.priority ?? 0) < priority,
  ).length;
  return before + 1;
}

const isOn = (mode?: string) => !!mode && mode !== "off";

export function describeStrategy(set: B4SetConfig, t: TFn): string {
  const S = (key: string, opts?: Record<string, unknown>) =>
    t(`discovery.strategy.${key}`, opts);
  const parts: string[] = [];
  const split = set.fragmentation?.strategy ?? "none";
  const faking = !!set.faking?.sni;
  const pinned = hasPins(set);

  if (split !== "none") {
    parts.push(S(`split.${split}`));
    if (faking) parts.push(S("fake"));
  } else if (faking) {
    parts.push(S("fakeOnly"));
  } else if (pinned) {
    parts.push(S("pinsOnly"));
  } else if (set.dns?.enabled) {
    parts.push(S("dnsOnly"));
  } else {
    parts.push(S("split.none"));
  }

  const tcp = set.tcp;
  if (tcp?.drop_sack) parts.push(S("extras.sack"));
  if (isOn(tcp?.desync?.mode)) parts.push(S("extras.desync"));
  if (isOn(tcp?.incoming?.mode)) parts.push(S("extras.incoming"));
  if (isOn(tcp?.win?.mode)) parts.push(S("extras.window"));
  if ((tcp?.seg2delay ?? 0) > 0) parts.push(S("extras.delay"));
  if (faking && (set.faking.tcp_md5 || set.faking.md5_on_fake)) {
    parts.push(S("extras.md5"));
  }
  if (set.dns?.enabled && (split !== "none" || faking || pinned)) {
    parts.push(S("extras.dns"));
  }
  if (pinned && (split !== "none" || faking)) parts.push(S("extras.pins"));

  const sentence = parts.join(S("join"));
  return sentence.charAt(0).toUpperCase() + sentence.slice(1) + S("end");
}

export function strategySuffix(set: B4SetConfig): string {
  let suffix = "";
  if (set.targets?.tls) suffix += `-tls${set.targets.tls.replace(".", "")}`;
  if (set.targets?.ip_version) suffix += `-ipv${set.targets.ip_version}`;
  return suffix;
}

export function historyVerdict(e: HistoryEntry): DiscoveryOutcome {
  const v = verdictOf(e, true);
  return v === "checking" ? "not_found" : v;
}

export function historySet(e: HistoryEntry): B4SetConfig | null {
  if (e.set) return normalizeSet(e.set);
  const raw = e.results?.[e.best_preset]?.set;
  if (!raw) return null;
  return scopeSet(raw, [e.domain], !!e.dns_result?.is_poisoned);
}

export function historyUnconfirmed(e: HistoryEntry): boolean {
  if (e.unconfirmed !== undefined) return e.unconfirmed;
  if (e.status === "canceled") return true;
  const tries = e.confirm_tries ?? 0;
  return !tries || (e.confirmed ?? 0) < tries;
}

export function formatDuration(
  t: TFn,
  startStr: string,
  endStr?: string,
): string {
  const start = new Date(startStr).getTime();
  const end = endStr ? new Date(endStr).getTime() : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return "";
  const total = Math.floor((end - start) / 1000);
  const min = Math.floor(total / 60);
  const sec = total % 60;
  if (min === 0) return t("discovery.duration.seconds", { sec });
  return t("discovery.duration.minutes", { min, sec });
}

export function formatTimeAgo(
  t: TFn,
  dateStr: string,
  fallback?: string,
): string {
  let date = new Date(dateStr);
  if (Number.isNaN(date.getTime()) || date.getFullYear() < 1970) {
    if (fallback) {
      date = new Date(fallback);
    }
    if (Number.isNaN(date.getTime()) || date.getFullYear() < 1970) {
      return "";
    }
  }
  const diff = Date.now() - date.getTime();
  if (diff < 0) return t("core.timeAgo.justNow");
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return t("core.timeAgo.justNow");
  if (minutes < 60) return t("core.timeAgo.minutesAgo", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("core.timeAgo.hoursAgo", { count: hours });
  const days = Math.floor(hours / 24);
  if (days < 30) return t("core.timeAgo.daysAgo", { count: days });
  return t("core.timeAgo.monthsAgo", { count: Math.floor(days / 30) });
}

export function formatSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return "";
  const kb = bytesPerSec / 1024;
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB/s`;
  return `${kb.toFixed(0)} KB/s`;
}

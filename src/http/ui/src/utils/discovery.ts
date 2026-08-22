import { B4SetConfig } from "@models/config";
import {
  StrategyFamily,
  DiscoveryResult,
  BackendStrategyGroup,
} from "@models/discovery";

export interface StrategyGroup {
  family: StrategyFamily;
  winnerPreset: string;
  domains: {
    domain: string;
    speed: number;
    presetName: string;
  }[];
  minSpeed: number;
  maxSpeed: number;
  representativeSet: B4SetConfig | null;
}

export function groupByStrategy(
  results: Record<string, DiscoveryResult>,
  backendGroups?: BackendStrategyGroup[],
): {
  success: StrategyGroup[];
  failed: DiscoveryResult[];
  notBlocked: DiscoveryResult[];
} {
  const failed: DiscoveryResult[] = [];
  const notBlocked: DiscoveryResult[] = [];
  const grouped = new Set<string>();
  const success: StrategyGroup[] = [];

  if (backendGroups && backendGroups.length > 0) {
    backendGroups.forEach((bg) => {
      const domains = bg.domains.map((d) => {
        grouped.add(d);
        const dr = results[d];
        const winnerResult = dr?.results?.[bg.winner_preset];
        const speed = winnerResult?.speed ?? dr?.best_speed ?? 0;
        return {
          domain: d,
          speed,
          presetName: dr?.best_preset || bg.winner_preset,
        };
      });
      const speeds = domains.map((d) => d.speed).filter((s) => s > 0);
      success.push({
        family: bg.family,
        winnerPreset: bg.winner_preset,
        domains,
        minSpeed: speeds.length ? Math.min(...speeds) : 0,
        maxSpeed: speeds.length ? Math.max(...speeds) : 0,
        representativeSet: bg.set ?? null,
      });
    });
  }

  // Fallback for ungrouped successful domains (e.g. mid-cancel, before backend
  // has finished building groups). Groups by winning preset so each domain still
  // shows up as a success with an applicable set.
  const fallback: Record<string, StrategyGroup> = {};
  Object.values(results).forEach((dr) => {
    if (dr.baseline_works) {
      notBlocked.push(dr);
      return;
    }
    if (!dr.best_success || !dr.best_preset) {
      failed.push(dr);
      return;
    }
    if (grouped.has(dr.domain)) return;
    const bestResult = dr.results[dr.best_preset];
    if (!bestResult) {
      failed.push(dr);
      return;
    }
    const family = bestResult.family || "none";
    const key = `${family}::${dr.best_preset}`;
    if (!fallback[key]) {
      fallback[key] = {
        family,
        winnerPreset: dr.best_preset,
        domains: [],
        minSpeed: dr.best_speed,
        maxSpeed: dr.best_speed,
        representativeSet: bestResult.set || null,
      };
    }
    fallback[key].domains.push({
      domain: dr.domain,
      speed: dr.best_speed,
      presetName: dr.best_preset,
    });
    fallback[key].maxSpeed = Math.max(fallback[key].maxSpeed, dr.best_speed);
    fallback[key].minSpeed = Math.min(fallback[key].minSpeed, dr.best_speed);
  });
  Object.values(fallback).forEach((g) => success.push(g));

  return { success, failed, notBlocked };
}

export function formatTimeAgo(
  t: (key: string, opts?: Record<string, unknown>) => string,
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

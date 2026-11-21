import type { B4SetConfig } from "@models/config";
import { portFilterCovers } from "./ports";
import { declaresIpTargets } from "./targets";

const REFUSALS = [
  "window",
  "targets",
  "domains",
  "tls",
  "port",
  "escalation",
] as const;

export type WSSizeReuseRefusal = (typeof REFUSALS)[number];
export type WSSizeReuseInactive = "tun" | "skip_setup" | "queue_mark";
export type WSSizeReuseIssue = WSSizeReuseRefusal | WSSizeReuseInactive;

export interface WSSizeReuseEnv {
  tunEngine: boolean;
  skipSetup: boolean;
  queueMark?: number;
}

export const SELF_DIAL_RELAY_MARK = 0x240000;
const DEFAULT_QUEUE_MARK = 0x8000;

export const effectiveQueueMark = (mark?: number) => mark || DEFAULT_QUEUE_MARK;

export const reuseTunEngine = (savedMode: string | undefined, runningTun: boolean) =>
  savedMode === "tun" || runningTun;

export const isWSSizeReuseRefusal = (
  issue: WSSizeReuseIssue,
): issue is WSSizeReuseRefusal =>
  (REFUSALS as readonly string[]).includes(issue);

const listed = (values?: string[]) => (values?.length ?? 0) > 0;

const refusals = (set: B4SetConfig): WSSizeReuseRefusal[] => {
  const ws = set.tcp.wssize;
  const targets = set.targets;
  const out: WSSizeReuseRefusal[] = [];
  if (!ws?.enabled || !ws.window) out.push("window");
  if (!declaresIpTargets(targets)) out.push("targets");
  if (
    targets.domain_only ||
    listed(targets.sni_domains) ||
    listed(targets.geosite_categories)
  ) {
    out.push("domains");
  }
  if (targets.tls) out.push("tls");
  if (!portFilterCovers(set.tcp.dport_filter, 443)) out.push("port");
  if (set.escalate?.to) out.push("escalation");
  return out;
};

const inactiveReason = (env: WSSizeReuseEnv): WSSizeReuseInactive | null => {
  if (env.tunEngine) return "tun";
  if (env.skipSetup) return "skip_setup";
  if ((effectiveQueueMark(env.queueMark) & SELF_DIAL_RELAY_MARK) !== 0) {
    return "queue_mark";
  }
  return null;
};

export const wssizeReuseIssues = (
  set: B4SetConfig,
  env: WSSizeReuseEnv,
): WSSizeReuseIssue[] => {
  if (!set.enabled || !set.tcp.wssize_reuse?.enabled) return [];
  const refused = refusals(set);
  if (refused.length > 0 || set.routing?.enabled || set.tcp.duplicate?.enabled) {
    return refused;
  }
  const reason = inactiveReason(env);
  return reason ? [reason] : [];
};

import type { TFunction } from "i18next";
import type {
  Attention,
  B4Event,
  EngineMode,
  EventCode,
  UpstreamAttention,
} from "../../models/metrics";
import type { SetWatchState, SetWatchStatus } from "../../models/watchdog";
import type { TelegramBridgeStatus } from "../../models/mtproto";
import { formatInteger } from "../common/charts/format";
import { describeEvent } from "./panels/eventText";
import { formatSince } from "./panels/format";
import { formatPercent } from "./statusFormat";

export type AttentionLevel = "error" | "warning";

export type AttentionAction =
  | { kind: "link"; to: string; label: string }
  | { kind: "restart"; label: string };

export interface AttentionItem {
  key: string;
  level: AttentionLevel;
  text: string;
  detail?: string;
  detailIsText?: boolean;
  time?: number;
  action?: AttentionAction;
  dismiss?: string;
}

export interface WatchedSet {
  id: string;
  name: string;
}

export interface WatchdogView {
  loaded: boolean;
  enabled: boolean;
  byId: ReadonlyMap<string, SetWatchStatus>;
}

export interface SystemView {
  hostHasIPv6: boolean;
  ipv6BypassesSets: boolean;
}

export interface ThreadView {
  os_threads: number;
  thread_warn: number;
  thread_limit: number;
}

export interface AttentionInput {
  t: TFunction;
  locale: string;
  now: number;
  mode: EngineMode | undefined;
  watched: readonly WatchedSet[];
  watchdog: WatchdogView;
  attention: Attention | undefined;
  threads: ThreadView | undefined;
  errors: readonly B4Event[];
  system: SystemView | undefined;
  bridge: TelegramBridgeStatus | undefined;
  dismissed: ReadonlySet<string>;
}

export const IPV6_DISMISS = "ipv6";
export const CONNTRACK_WARN_SHARE = 0.9;

export const START_FAILURE_LINK: Partial<Record<EventCode, string>> = {
  socks5_failed: "/settings/general",
  web_failed: "/settings/general",
  web_tls_unusable: "/settings/general",
  mtproto_failed: "/settings/mtproto",
  targets_warning: "/sets",
};

type WatchProblem = "failing" | "healing" | "gaveUp" | "cantCheck";

const WATCH_PROBLEM: Partial<Record<SetWatchState, WatchProblem>> = {
  degraded: "failing",
  cooldown: "failing",
  heal_queued: "healing",
  healing: "healing",
  gave_up: "gaveUp",
  unverifiable: "cantCheck",
};

const LEVEL_RANK: Record<AttentionLevel, number> = { error: 0, warning: 1 };

export function startFailureKey(event: B4Event): string {
  return `${event.id}:${event.t}`;
}

function setPath(setId: string, tab: string): string {
  return `/sets/${encodeURIComponent(setId)}?tab=${tab}`;
}

function linkAction(t: TFunction, to: string, labelKey: string): AttentionAction {
  return { kind: "link", to, label: t(`dashboard.attention.action.${labelKey}`) };
}

function startFailureActionLabel(to: string): string {
  if (to === "/settings/mtproto") return "telegram";
  if (to === "/sets") return "sets";
  return "settings";
}

function watchdogItems(input: AttentionInput, items: AttentionItem[]): void {
  const { t, watched, watchdog } = input;
  if (watched.length === 0 || !watchdog.loaded) return;
  if (!watchdog.enabled) {
    items.push({
      key: "watchdog-off",
      level: "warning",
      text: t("dashboard.attention.watchdogOff", {
        count: watched.length,
        value: formatInteger(watched.length, input.locale),
      }),
      action: linkAction(t, "/watchdog", "watchdog"),
    });
    return;
  }
  for (const set of watched) {
    const status = watchdog.byId.get(set.id);
    if (!status) continue;
    const problem = WATCH_PROBLEM[status.status];
    if (!problem) continue;
    const reason = status.reason
      ? t(`watchdog.reason.${status.reason}`, { defaultValue: "" })
      : "";
    items.push({
      key: `watch-${set.id}`,
      level: problem === "gaveUp" ? "error" : "warning",
      text: t(`dashboard.attention.watch.${problem}`, {
        set: set.name || status.set_name || set.id,
      }),
      detail: reason || undefined,
      detailIsText: true,
      action: linkAction(t, setPath(set.id, "discovery"), "set"),
    });
  }
}

function ipv6Item(input: AttentionInput, items: AttentionItem[]): void {
  const { t, system, mode, dismissed } = input;
  if (!system || dismissed.has(IPV6_DISMISS)) return;
  const tun = mode === "tun" && system.hostHasIPv6;
  if (!tun && !system.ipv6BypassesSets) return;
  items.push({
    key: "ipv6",
    level: "warning",
    text: t(tun ? "dashboard.attention.ipv6Tun" : "dashboard.attention.ipv6"),
    action: linkAction(t, "/settings/general", "settings"),
    dismiss: IPV6_DISMISS,
  });
}

function bridgeItems(input: AttentionInput, items: AttentionItem[]): void {
  const { t, bridge } = input;
  if (!bridge?.enabled) return;
  const action = linkAction(t, "/settings/mtproto", "telegram");
  const k = (name: string) => `dashboard.attention.bridge.${name}`;
  const { listener, tproxy, addresses } = bridge;
  if (listener && !listener.running && listener.error) {
    items.push({
      key: "bridge-listener",
      level: "error",
      text: t(k("listener"), { port: String(listener.port || "-") }),
      detail: listener.error,
      action,
    });
  }
  if (!bridge.rule_installed) {
    items.push({
      key: "bridge-rule",
      level: bridge.skip_setup ? "warning" : "error",
      text: t(k(bridge.skip_setup ? "skipSetup" : "rule")),
      action,
    });
  } else if (bridge.rule_shadowed_by) {
    items.push({
      key: "bridge-shadowed",
      level: "error",
      text: t(k("shadowed")),
      detail: bridge.rule_shadowed_by,
      action,
    });
  }
  if (tproxy?.checked && !tproxy.available) {
    const missing = [...(tproxy.packages ?? []), ...(tproxy.missing ?? [])];
    items.push({
      key: "bridge-tproxy",
      level: "error",
      text: t(k("tproxy")),
      detail: missing.length > 0 ? missing.join(", ") : undefined,
      action,
    });
  }
  if (addresses?.last_error) {
    items.push({
      key: "bridge-addresses",
      level: "warning",
      text: t(k("addresses")),
      detail: addresses.last_error,
      action,
    });
  }
  const bridges = bridge.bridge_netfilter ?? [];
  if (bridges.length > 0) {
    items.push({
      key: "bridge-netfilter",
      level: "warning",
      text: t(k("netfilter"), { bridges: bridges.join(", ") }),
      action,
    });
  }
}

function upstreamItem(
  input: AttentionInput,
  upstream: UpstreamAttention,
): AttentionItem {
  const { t, locale, now } = input;
  const failures = t("dashboard.attention.failures", {
    count: upstream.failures,
    value: formatInteger(upstream.failures, locale),
  });
  const values = {
    set: upstream.set_name || upstream.set_id,
    upstream: upstream.upstream,
    failures,
    time: formatSince(upstream.last_failure, now, locale),
  };
  return {
    key: `upstream-${upstream.set_id}-${upstream.upstream}`,
    level: upstream.fail_open ? "warning" : "error",
    text: t(
      upstream.fail_open
        ? "dashboard.attention.upstreamOpen"
        : "dashboard.attention.upstreamClosed",
      values,
    ),
    detail: upstream.last_error || undefined,
    action: upstream.set_id
      ? linkAction(t, setPath(upstream.set_id, "routing"), "routing")
      : undefined,
  };
}

function startFailureItems(input: AttentionInput, items: AttentionItem[]): void {
  const { t, locale, errors, dismissed } = input;
  const seen = new Set<string>();
  for (const event of errors) {
    const to = START_FAILURE_LINK[event.code];
    if (!to) continue;
    const key = startFailureKey(event);
    if (seen.has(key) || dismissed.has(key)) continue;
    seen.add(key);
    const view = describeEvent(event, t, locale);
    items.push({
      key: `event-${key}`,
      level: "error",
      text: view.text,
      detail: event.args?.error || event.message || undefined,
      time: event.t,
      action: linkAction(t, to, startFailureActionLabel(to)),
      dismiss: key,
    });
  }
}

function overloadItems(input: AttentionInput, items: AttentionItem[]): void {
  const { t, locale } = input;
  const overload = input.attention?.overload;
  if (!overload) return;
  const minutes = Math.max(1, Math.round((overload.window_s || 600) / 60));
  const action = linkAction(t, "/settings/general", "settings");
  const entries: [string, number][] = [
    ["injectSkipped", overload.inject_skipped],
    ["rawSendDropped", overload.raw_send_dropped],
    ["queueOverflow", overload.queue_overflow],
  ];
  for (const [name, count] of entries) {
    if (!(count > 0)) continue;
    items.push({
      key: `overload-${name}`,
      level: "warning",
      text: t(`dashboard.attention.overload.${name}`, {
        count,
        value: formatInteger(count, locale),
        minutes: formatInteger(minutes, locale),
      }),
      action,
    });
  }
}

function conntrackItem(input: AttentionInput, items: AttentionItem[]): void {
  const { t, locale } = input;
  const conntrack = input.attention?.conntrack;
  if (!conntrack || !(conntrack.max > 0)) return;
  const share = conntrack.count / conntrack.max;
  if (!(share >= CONNTRACK_WARN_SHARE)) return;
  items.push({
    key: "conntrack",
    level: "warning",
    text: t("dashboard.attention.conntrack", {
      percent: formatPercent(Math.round(share * 100), locale),
    }),
    detail: t("dashboard.attention.conntrackDetail", {
      count: conntrack.count,
      value: formatInteger(conntrack.count, locale),
      max: formatInteger(conntrack.max, locale),
    }),
    detailIsText: true,
  });
}

function threadItem(input: AttentionInput, items: AttentionItem[]): void {
  const { t, locale, threads } = input;
  if (!threads || !(threads.thread_warn > 0)) return;
  if (!(threads.os_threads >= threads.thread_warn)) return;
  items.push({
    key: "threads",
    level: "warning",
    text: t("dashboard.attention.threads", {
      value: formatInteger(threads.os_threads, locale),
      limit: formatInteger(threads.thread_limit, locale),
    }),
    action: linkAction(t, "/logs", "logs"),
  });
}

export function buildAttentionItems(input: AttentionInput): AttentionItem[] {
  const { t } = input;
  const items: AttentionItem[] = [];
  for (const upstream of input.attention?.upstreams ?? []) {
    if (upstream.failures > 0) items.push(upstreamItem(input, upstream));
  }
  bridgeItems(input, items);
  watchdogItems(input, items);
  startFailureItems(input, items);
  if (input.attention?.binary_replaced) {
    items.push({
      key: "binary-replaced",
      level: "warning",
      text: t("dashboard.attention.binaryReplaced"),
      action: { kind: "restart", label: t("dashboard.attention.action.restart") },
    });
  }
  overloadItems(input, items);
  conntrackItem(input, items);
  threadItem(input, items);
  ipv6Item(input, items);
  return items
    .map((item, index) => ({ item, index }))
    .sort(
      (a, b) =>
        LEVEL_RANK[a.item.level] - LEVEL_RANK[b.item.level] || a.index - b.index,
    )
    .map(({ item }) => item);
}

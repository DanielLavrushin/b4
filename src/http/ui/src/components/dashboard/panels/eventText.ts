import type { TFunction } from "i18next";
import type { B4Event } from "@models/metrics";
import { formatClock, formatInteger } from "../../common/charts/format";
import { parseCount } from "./format";

export type EventLinkKind = "logs" | "set" | "sets" | "mcp";

export interface EventLink {
  to: string;
  kind: EventLinkKind;
}

export interface EventView {
  text: string;
  detail?: string;
  detailIsText?: boolean;
  link?: EventLink;
}

const ENGINE_LABEL: Record<string, string> = {
  nfqueue: "NFQUEUE",
  tun: "TUN",
};

export function engineLabel(engine: string | undefined): string {
  if (!engine) return "-";
  return ENGINE_LABEL[engine] ?? engine.toUpperCase();
}

const LOGS: EventLink = { to: "/logs", kind: "logs" };

function setLink(setId: string | undefined, tab?: string): EventLink | undefined {
  if (!setId) return undefined;
  const base = `/sets/${encodeURIComponent(setId)}`;
  return { to: tab ? `${base}?tab=${tab}` : base, kind: "set" };
}

function countText(
  t: TFunction,
  key: string,
  raw: string | undefined,
  locale: string,
): string {
  const n = parseCount(raw) ?? 0;
  return t(key, { count: n, value: formatInteger(n, locale) });
}

export function describeEvent(
  event: B4Event,
  t: TFunction,
  locale: string,
): EventView {
  const args = event.args ?? {};
  const k = (name: string) => `dashboard.events.code.${name}`;
  const error = args.error || undefined;
  switch (event.code) {
    case "started": {
      const name = args.version ? `b4 ${args.version}` : "b4";
      const threads = parseCount(args.threads);
      if (threads === null) {
        return { text: t(k("startedShort"), { name, engine: engineLabel(args.engine) }) };
      }
      return {
        text: t(k("started"), {
          name,
          engine: engineLabel(args.engine),
          count: threads,
          threads: formatInteger(threads, locale),
        }),
      };
    }
    case "engine_failed":
      return {
        text: t(k("engine_failed"), { engine: engineLabel(args.engine) }),
        detail: error,
        link: LOGS,
      };
    case "settings_applied":
      return {
        text: t(k("settings_applied"), {
          sets: countText(t, "dashboard.events.count.sets", args.sets, locale),
          domains: countText(t, "dashboard.events.count.domains", args.domains, locale),
          ips: countText(t, "dashboard.events.count.ips", args.ips, locale),
        }),
        link: { to: "/sets", kind: "sets" },
      };
    case "targets_warning":
    case "socks5_failed":
    case "mtproto_failed":
    case "web_failed":
    case "web_tls_unusable":
      return { text: t(k(event.code)), detail: error, link: LOGS };
    case "rules_restored": {
      const n = parseCount(args.count) ?? 1;
      if (n <= 1) return { text: t(k("rulesRestoredOnce")) };
      return {
        text: t(k("rules_restored"), {
          count: n,
          value: formatInteger(n, locale),
          time: formatClock(event.t, locale),
        }),
      };
    }
    case "watchdog_healed":
      return {
        text: t(k("watchdog_healed"), {
          set: args.set || args.set_id || "-",
          preset: args.preset || "-",
        }),
        link: setLink(args.set_id),
      };
    case "watchdog_gave_up": {
      const reason = args.reason
        ? t(`watchdog.reason.${args.reason}`, { defaultValue: args.reason })
        : undefined;
      return {
        text: t(k("watchdog_gave_up"), { set: args.set || args.set_id || "-" }),
        detail: reason,
        detailIsText: true,
        link: setLink(args.set_id, "discovery"),
      };
    }
    case "mcp_write":
      return {
        text: t(k("mcp_write")),
        detail: args.path || undefined,
        link: { to: "/settings/api", kind: "mcp" },
      };
    case "update_installed":
      return args.version
        ? { text: t(k("update_installed"), { version: args.version }) }
        : { text: t(k("updateInstalledPlain")) };
    default:
      return {
        text: event.message || event.code || "-",
        detail: error,
        link: event.level === "error" ? LOGS : undefined,
      };
  }
}

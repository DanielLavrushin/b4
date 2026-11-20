import type { TFunction } from "i18next";
import { B4Config, B4SetConfig } from "@models/config";

export type TelegramSetProblem =
  | { kind: "missing" }
  | {
      kind: "disabled" | "unusable" | "incomplete" | "tun" | "mark";
      name: string;
    };

const TELEGRAM_SET_MARK = 0x4040000;
const DEFAULT_QUEUE_MARK = 0x8000;

const carriesTelegramSetMark = (mark: number) =>
  (mark & TELEGRAM_SET_MARK) === TELEGRAM_SET_MARK;

const telegramSetMarkConflict = (config: B4Config) => {
  const queue = config.queue.mark ?? 0;
  if (queue !== 0 && (queue & ~TELEGRAM_SET_MARK) === 0) return true;
  const main = queue || DEFAULT_QUEUE_MARK;
  const checker = config.system.checker;
  return (
    carriesTelegramSetMark(checker?.discovery_flow_mark || main + 1) ||
    carriesTelegramSetMark(checker?.discovery_injected_mark || main + 2)
  );
};

const routingIncomplete = (set: B4SetConfig) => {
  if (!set.routing?.enabled) return false;
  const mode = set.routing.mode || "interface";
  if (mode === "interface") return !set.routing.egress_interface;
  if (mode === "proxy") return !(set.routing.upstream?.port > 0);
  return false;
};

export const carriesTelegram = (set: B4SetConfig) =>
  !set.routing?.enabled ||
  (set.routing.mode !== "mtproto-ws" && set.routing.mode !== "block");

export const telegramSetId = (config: B4Config) =>
  config.system.mtproto?.upstream_set ?? "";

export const telegramSetProblem = (
  config: B4Config,
): TelegramSetProblem | null => {
  const id = telegramSetId(config);
  if (id === "") return null;
  const set = config.sets.find((s) => s.id === id);
  if (!set) return { kind: "missing" };
  if (!set.enabled) return { kind: "disabled", name: set.name };
  if (!carriesTelegram(set)) return { kind: "unusable", name: set.name };
  if (routingIncomplete(set)) return { kind: "incomplete", name: set.name };
  if (config.queue.mode === "tun") return { kind: "tun", name: set.name };
  if (telegramSetMarkConflict(config)) return { kind: "mark", name: set.name };
  return null;
};

export const telegramSetUsable = (config: B4Config) =>
  telegramSetId(config) !== "" && telegramSetProblem(config) === null;

const portNumber = (text: string) =>
  /^[+-]?\d+$/.test(text) ? Number(text) : Number.NaN;

const portRange = (part: string): [number, number] | null => {
  const text = part.trim();
  if (text === "") return null;
  const dash = text.indexOf("-");
  if (dash >= 0) {
    const low = portNumber(text.slice(0, dash));
    const high = portNumber(text.slice(dash + 1));
    return low >= 1 && low <= high && high <= 65535 ? [low, high] : null;
  }
  const port = portNumber(text);
  return port >= 1 && port <= 65535 ? [port, port] : null;
};

export const setTakesTCPPort = (set: B4SetConfig | undefined, port: number) => {
  const ranges = (set?.tcp?.dport_filter ?? "")
    .split(",")
    .map(portRange)
    .filter((range) => range !== null);
  return (
    ranges.length === 0 ||
    ranges.some(([low, high]) => port >= low && port <= high)
  );
};

export const telegramSetRoutes = (config: B4Config) =>
  telegramSetUsable(config) &&
  config.sets.some(
    (s) => s.id === telegramSetId(config) && !!s.routing?.enabled,
  );

const PROBLEM_TEXT_KEYS: Record<TelegramSetProblem["kind"], string> = {
  missing: "settings.MTProto.upstreamSetMissing",
  disabled: "settings.MTProto.upstreamSetDisabled",
  unusable: "settings.MTProto.upstreamSetUnusable",
  incomplete: "settings.MTProto.upstreamSetIncomplete",
  tun: "settings.MTProto.upstreamSetTun",
  mark: "settings.MTProto.upstreamSetMark",
};

export const telegramSetProblemText = (
  t: TFunction,
  problem: TelegramSetProblem | null,
) => {
  if (!problem) return "";
  if (problem.kind === "missing") return t(PROBLEM_TEXT_KEYS.missing);
  return t(PROBLEM_TEXT_KEYS[problem.kind], { name: problem.name });
};

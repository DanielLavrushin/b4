import type { TFunction } from "i18next";
import { B4Config, B4SetConfig } from "@models/config";

export type TelegramSetProblem =
  | { kind: "missing" }
  | {
      kind: "disabled" | "unusable" | "incomplete" | "mark";
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
  if (telegramSetMarkConflict(config)) return { kind: "mark", name: set.name };
  return null;
};

export const telegramSetUsable = (config: B4Config) =>
  telegramSetId(config) !== "" && telegramSetProblem(config) === null;

const PROBLEM_TEXT_KEYS: Record<TelegramSetProblem["kind"], string> = {
  missing: "settings.MTProto.upstreamSetMissing",
  disabled: "settings.MTProto.upstreamSetDisabled",
  unusable: "settings.MTProto.upstreamSetUnusable",
  incomplete: "settings.MTProto.upstreamSetIncomplete",
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

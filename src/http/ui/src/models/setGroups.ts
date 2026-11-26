import { isHubEnvelope } from "./hub";
import type { HubEnvelope } from "./hub";

export interface GroupedSet {
  id: string;
  group?: string;
}

export interface SetBlock<T> {
  id: string;
  group: string;
  sets: T[];
}

export interface SetGroupDocument {
  group: string;
  sets: Record<string, unknown>[];
}

export type SetsImport =
  | { kind: "empty" }
  | { kind: "command" }
  | { kind: "sets"; group: string; sets: Record<string, unknown>[] }
  | { kind: "envelope"; envelope: HubEnvelope }
  | { kind: "invalid"; reason: "json" | "config" | "unknown" };

export type Lean = -1 | 0 | 1;

export interface SlotRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface DropResult {
  ids: string[];
  group: string;
}

const GROUP_BLOCK_PREFIX = "group:";
const LEAN_REACH = 0.15;
const SET_ROOT_KEYS = ["name", "tcp", "udp", "targets", "fragmentation", "faking"];

const groupOf = (set: GroupedSet | undefined): string => set?.group ?? "";

export function setBlocks<T extends GroupedSet>(sets: T[]): SetBlock<T>[] {
  const blocks: SetBlock<T>[] = [];
  for (const set of sets) {
    const group = groupOf(set);
    const last = blocks.at(-1);
    if (group !== "" && last?.group === group) {
      last.sets.push(set);
      continue;
    }
    blocks.push({
      id: group === "" ? set.id : `${GROUP_BLOCK_PREFIX}${set.id}`,
      group,
      sets: [set],
    });
  }
  return blocks;
}

export function nearestSlot(center: Point, slots: SlotRect[]): number {
  let best = 0;
  let bestDistance = Infinity;
  slots.forEach((slot, index) => {
    const dx = center.x - (slot.left + slot.width / 2);
    const dy = center.y - (slot.top + slot.height / 2);
    const distance = dx * dx + dy * dy;
    if (distance < bestDistance) {
      best = index;
      bestDistance = distance;
    }
  });
  return best;
}

export function leanOf(center: Point, slot: SlotRect, vertical: boolean): Lean {
  const offset = vertical
    ? center.y - (slot.top + slot.height / 2)
    : center.x - (slot.left + slot.width / 2);
  const reach = LEAN_REACH * (vertical ? slot.height : slot.width);
  if (offset < -reach) return -1;
  return offset > reach ? 1 : 0;
}

const runEnds = (list: GroupedSet[], index: number): [number, number] => {
  const group = groupOf(list[index]);
  let start = index;
  let end = index;
  if (group === "") return [start, end];
  while (start > 0 && groupOf(list[start - 1]) === group) start--;
  while (end < list.length - 1 && groupOf(list[end + 1]) === group) end++;
  return [start, end];
};

const outsideRuns = (
  others: GroupedSet[],
  prev: GroupedSet | undefined,
  next: GroupedSet | undefined,
  fallback: number,
): number => {
  if (prev) return runEnds(others, others.indexOf(prev))[1] + 1;
  if (next) return runEnds(others, others.indexOf(next))[0];
  return fallback;
};

const placed = (
  others: GroupedSet[],
  moving: GroupedSet[],
  index: number,
): string[] =>
  [...others.slice(0, index), ...moving, ...others.slice(index)].map(
    (set) => set.id,
  );

export function cardDrop(
  sets: GroupedSet[],
  visibleIds: string[],
  activeId: string,
  slot: number,
  lean: Lean,
): DropResult {
  const home = sets.findIndex((set) => set.id === activeId);
  const unchanged = { ids: sets.map((set) => set.id), group: "" };
  if (home < 0) return unchanged;
  const active = sets[home];
  const own = groupOf(active);
  unchanged.group = own;

  const shown = new Set(visibleIds);
  const others = sets.filter((set) => set.id !== activeId);
  const seen = others.filter((set) => shown.has(set.id));
  const homeSlot = sets
    .filter((set) => shown.has(set.id))
    .findIndex((set) => set.id === activeId);
  const at = Math.max(0, Math.min(slot, seen.length));
  const prev = seen[at - 1];
  const next = seen[at];
  const before = groupOf(prev);
  const after = groupOf(next);
  const alone = own !== "" && !others.some((set) => groupOf(set) === own);
  const detached = alone ? own : "";

  let group: string;
  if (before !== "" && before === after) group = before;
  else if (lean < 0) group = before || detached;
  else if (lean > 0) group = after || detached;
  else group = own !== "" && (own === before || own === after) ? own : detached;

  if (at === homeSlot && group === own) return unchanged;

  let index: number;
  if (prev && group !== "" && group === before) {
    index = others.indexOf(prev) + 1;
  } else if (next && group !== "" && group === after) {
    index = others.indexOf(next);
  } else {
    index = outsideRuns(others, prev, next, home);
  }
  return { ids: placed(others, [active], index), group };
}

export function groupDrop(
  sets: GroupedSet[],
  visibleIds: string[],
  runIds: string[],
  slot: number,
): string[] {
  const run = new Set(runIds);
  const unchanged = sets.map((set) => set.id);
  const moving = sets.filter((set) => run.has(set.id));
  if (moving.length === 0) return unchanged;

  const shown = new Set(visibleIds);
  const others = sets.filter((set) => !run.has(set.id));
  const seen = others.filter((set) => shown.has(set.id));
  const homeSlot = sets
    .filter((set) => shown.has(set.id))
    .findIndex((set) => run.has(set.id));
  let at = Math.max(0, Math.min(slot, seen.length));

  const inside = groupOf(seen[at - 1]);
  if (inside !== "" && inside === groupOf(seen[at])) {
    let start = at - 1;
    let end = at;
    while (start > 0 && groupOf(seen[start - 1]) === inside) start--;
    while (end < seen.length - 1 && groupOf(seen[end + 1]) === inside) end++;
    at = at - start <= end + 1 - at ? start : end + 1;
  }
  if (at === homeSlot) return unchanged;

  return placed(others, moving, outsideRuns(others, seen[at - 1], seen[at], 0));
}

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const looksLikeSet = (value: unknown): value is Record<string, unknown> =>
  isObject(value) && SET_ROOT_KEYS.some((key) => key in value);

function severalJsonValues(text: string): unknown[] | null {
  const values: unknown[] = [];
  let depth = 0;
  let start = 0;
  let quoted = false;
  let escaped = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (quoted) {
      if (escaped) escaped = false;
      else if (ch === "\\") escaped = true;
      else if (ch === '"') quoted = false;
      continue;
    }
    if (ch === '"') {
      if (depth === 0) return null;
      quoted = true;
    } else if (ch === "{" || ch === "[") {
      if (depth === 0) start = i;
      depth++;
    } else if (ch === "}" || ch === "]") {
      depth--;
      if (depth < 0) return null;
      if (depth > 0) continue;
      try {
        values.push(JSON.parse(text.slice(start, i + 1)));
      } catch {
        return null;
      }
    } else if (depth === 0 && !/[\s,]/.test(ch)) {
      return null;
    }
  }
  return depth === 0 && values.length > 0 ? values : null;
}

function jsonValues(text: string): unknown[] | null {
  try {
    const value: unknown = JSON.parse(text);
    return [value];
  } catch {
    return severalJsonValues(text);
  }
}

const setsImport = (list: unknown[], group: string): SetsImport =>
  list.length > 0 && list.every(looksLikeSet)
    ? { kind: "sets", group, sets: list }
    : { kind: "invalid", reason: "unknown" };

export function parseSetsImport(text: string): SetsImport {
  const trimmed = text.trim();
  if (trimmed === "") return { kind: "empty" };
  const values = jsonValues(trimmed);
  if (!values) {
    return trimmed.startsWith("{")
      ? { kind: "invalid", reason: "json" }
      : { kind: "command" };
  }
  if (values.length > 1) return setsImport(values, "");
  const value = values[0];
  if (isHubEnvelope(value)) return { kind: "envelope", envelope: value };
  if (Array.isArray(value)) return setsImport(value, "");
  if (isObject(value) && Array.isArray(value.sets)) {
    if ("system" in value || "queue" in value || "version" in value) {
      return { kind: "invalid", reason: "config" };
    }
    const list: unknown[] = value.sets;
    return setsImport(
      list,
      typeof value.group === "string" ? value.group.trim() : "",
    );
  }
  return setsImport([value], "");
}

export function setGroupFileName(group: string): string {
  const safe = group
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-+|-+$/g, "");
  return `${safe || "group"}.b4group.json`;
}

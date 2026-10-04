import type { Projection } from "@/models/api";

export type DiffKind = "added" | "removed" | "changed";

export interface DiffLine {
  path: string;
  kind: DiffKind;
  before?: string;
  after?: string;
}

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isString = (value: unknown): value is string => typeof value === "string";

const show = (value: unknown): string => {
  if (value === undefined) return "";
  if (typeof value === "string") return value;
  return JSON.stringify(value);
};

const strings = (value: unknown): string[] | null => {
  if (value === undefined) return [];
  if (!Array.isArray(value)) return null;
  const list: unknown[] = value;
  return list.every(isString) ? list : null;
};

const listLines = (before: unknown, after: unknown, path: string): DiffLine[] | null => {
  if (!Array.isArray(before) && !Array.isArray(after)) return null;
  const was = strings(before);
  const now = strings(after);
  if (was === null || now === null) return null;
  const wasSet = new Set(was);
  const nowSet = new Set(now);
  const seen = new Set<string>();
  const out: DiffLine[] = [];
  was.forEach((value) => {
    if (nowSet.has(value) || seen.has(value)) return;
    seen.add(value);
    out.push({ path, kind: "removed", before: value });
  });
  now.forEach((value) => {
    if (wasSet.has(value) || seen.has(value)) return;
    seen.add(value);
    out.push({ path, kind: "added", after: value });
  });
  return out.length > 0 ? out : null;
};

const objectOrNothing = (value: unknown): Record<string, unknown> | null => {
  if (value === undefined) return {};
  return isObject(value) ? value : null;
};

const walk = (
  before: unknown,
  after: unknown,
  path: string,
  out: DiffLine[],
) => {
  const same = JSON.stringify(before) === JSON.stringify(after);
  if (same) return;
  const was = objectOrNothing(before);
  const now = objectOrNothing(after);
  if (was !== null && now !== null) {
    const count = out.length;
    const keys = new Set([...Object.keys(was), ...Object.keys(now)]);
    [...keys].sort().forEach((key) => {
      walk(was[key], now[key], path ? `${path}.${key}` : key, out);
    });
    if (out.length > count) return;
  }
  const entries = listLines(before, after, path);
  if (entries) {
    out.push(...entries);
    return;
  }
  if (before === undefined) {
    out.push({ path, kind: "added", after: show(after) });
  } else if (after === undefined) {
    out.push({ path, kind: "removed", before: show(before) });
  } else {
    out.push({ path, kind: "changed", before: show(before), after: show(after) });
  }
};

export const diffProjections = (before: Projection, after: Projection): DiffLine[] => {
  const out: DiffLine[] = [];
  walk(before, after, "", out);
  return out;
};

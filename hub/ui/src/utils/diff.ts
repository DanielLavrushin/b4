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

const show = (value: unknown): string => {
  if (value === undefined) return "";
  if (typeof value === "string") return value;
  return JSON.stringify(value);
};

const walk = (
  before: unknown,
  after: unknown,
  path: string,
  out: DiffLine[],
) => {
  if (isObject(before) && isObject(after)) {
    const keys = new Set([...Object.keys(before), ...Object.keys(after)]);
    [...keys].sort().forEach((key) => {
      walk(before[key], after[key], path ? `${path}.${key}` : key, out);
    });
    return;
  }
  const same = JSON.stringify(before) === JSON.stringify(after);
  if (same) return;
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

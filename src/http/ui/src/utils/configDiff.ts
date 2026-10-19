export type ConfigLeafMap = Record<string, unknown>;

export interface ChangedLeaf {
  path: string;
  before: unknown;
  after: unknown;
}

export interface ConfigDiffOptions {
  ignoredKeys?: ReadonlySet<string>;
  ignoredRootKeys?: ReadonlySet<string>;
  ignoredPaths?: ReadonlySet<string>;
}

export const flattenConfigLeaves = (
  value: Record<string, unknown>,
  prefix = "",
  ignoredKeys?: ReadonlySet<string>,
  ignoredPaths?: ReadonlySet<string>,
  ignoredRootKeys?: ReadonlySet<string>,
): ConfigLeafMap => {
  const result: ConfigLeafMap = {};

  const visit = (current: unknown, path: string): void => {
    if (path && ignoredPaths?.has(path)) return;
    if (Array.isArray(current)) {
      if (path) result[path] = current;
      return;
    }
    if (
      typeof current === "object" &&
      current !== null
    ) {
      const isRoot = path === prefix;
      for (const [key, child] of Object.entries(
        current as Record<string, unknown>,
      )) {
        if (ignoredKeys?.has(key)) continue;
        if (isRoot && ignoredRootKeys?.has(key)) continue;
        visit(child, path ? `${path}.${key}` : key);
      }
      return;
    }
    if (path) result[path] = current;
  };

  visit(value, prefix);
  return result;
};

export const isZero = (value: unknown): boolean =>
  value === undefined ||
  value === null ||
  value === "" ||
  value === 0 ||
  value === false ||
  (Array.isArray(value) && value.length === 0);

// Order is user-visible state for these leaves: mirrors are tried in order,
// seq_overlap_pattern emits bytes positionally. Everywhere else arrays stay
// order-insensitive so set-like lists don't produce phantom dirty state.
export const ORDERED_ARRAY_PATHS: Record<string, true> = {
  "system.update.mirrors": true,
  "system.hub.urls": true,
  "tcp.win.values": true,
  "fragmentation.seq_overlap_pattern": true,
  "faking.sni_mutation.fake_snis": true,
};
export const leafEqual = (
  a: unknown,
  b: unknown,
  path?: string,
): boolean => {
  if (Object.is(a, b)) return true;
  if (isZero(a) && isZero(b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    if (path && ORDERED_ARRAY_PATHS[path]) {
      return a.every(
        (v, i) => JSON.stringify(v) === JSON.stringify(b[i]),
      );
    }
    const primitive = (v: unknown): boolean =>
      v === null || ["string", "number", "boolean"].includes(typeof v);
    if (a.every(primitive) && b.every(primitive)) {
      const sortedA = [...(a as (string | number | boolean)[])].sort();
      const sortedB = [...(b as (string | number | boolean)[])].sort();
      return sortedA.every((v, i) => v === sortedB[i]);
    }
    const sortedA = (a as unknown[]).map((v) => JSON.stringify(v)).sort();
    const sortedB = (b as unknown[]).map((v) => JSON.stringify(v)).sort();
    return sortedA.every((v, i) => v === sortedB[i]);
  }
  if (typeof a !== "object" && typeof b !== "object") return false;
  return JSON.stringify(a) === JSON.stringify(b);
};

export const diffConfigLeaves = (
  draft: Record<string, unknown>,
  baseline: Record<string, unknown>,
  options?: ConfigDiffOptions,
): ChangedLeaf[] => {
  if (draft === baseline) return [];
  const out: ChangedLeaf[] = [];
  // Mirrors flattenConfigLeaves: plain objects recurse, everything else
  // (incl. arrays) compares as a leaf — so leaf granularity is unchanged.
  const recurses = (v: unknown): v is Record<string, unknown> =>
    typeof v === "object" && v !== null && !Array.isArray(v);
  const walk = (d: unknown, b: unknown, path: string): void => {
    if (Object.is(d, b)) return;
    if (path && options?.ignoredPaths?.has(path)) return;
    const dRec = recurses(d);
    const bRec = recurses(b);
    if (!dRec && !bRec) {
      if (path && !leafEqual(d, b, path)) out.push({ path, before: b, after: d });
      return;
    }
    if (
      path &&
      ((dRec && !bRec && b !== undefined) || (!dRec && bRec && d !== undefined))
    ) {
      if (!leafEqual(d, b, path)) out.push({ path, before: b, after: d });
    }
    const dMap = dRec ? (d as Record<string, unknown>) : {};
    const bMap = bRec ? (b as Record<string, unknown>) : {};
    const isRoot = path === "";
    for (const key of new Set([...Object.keys(dMap), ...Object.keys(bMap)])) {
      if (options?.ignoredKeys?.has(key)) continue;
      if (isRoot && options?.ignoredRootKeys?.has(key)) continue;
      walk(dMap[key], bMap[key], path ? `${path}.${key}` : key);
    }
  };
  walk(draft, baseline, "");
  out.sort((a, b) => a.path.localeCompare(b.path));
  return out;
};

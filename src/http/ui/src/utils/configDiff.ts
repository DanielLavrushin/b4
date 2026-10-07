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

export const leafEqual = (a: unknown, b: unknown): boolean => {
  if (isZero(a) && isZero(b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    const sortedA = (a as unknown[]).map((v) => JSON.stringify(v)).sort();
    const sortedB = (b as unknown[]).map((v) => JSON.stringify(v)).sort();
    return sortedA.every((v, i) => v === sortedB[i]);
  }
  return JSON.stringify(a) === JSON.stringify(b);
};

export const diffConfigLeaves = (
  draft: Record<string, unknown>,
  baseline: Record<string, unknown>,
  options?: ConfigDiffOptions,
): ChangedLeaf[] => {
  const flatDraft = flattenConfigLeaves(
    draft,
    "",
    options?.ignoredKeys,
    options?.ignoredPaths,
    options?.ignoredRootKeys,
  );
  const flatBaseline = flattenConfigLeaves(
    baseline,
    "",
    options?.ignoredKeys,
    options?.ignoredPaths,
    options?.ignoredRootKeys,
  );
  const paths = Array.from(
    new Set([...Object.keys(flatDraft), ...Object.keys(flatBaseline)]),
  ).sort((a, b) => a.localeCompare(b));

  return paths.flatMap((path) => {
    const before = flatBaseline[path];
    const after = flatDraft[path];
    if (leafEqual(after, before)) return [];
    return [{ path, before, after }];
  });
};

export const changedConfigPaths = (
  draft: Record<string, unknown>,
  baseline: Record<string, unknown>,
  options?: ConfigDiffOptions,
): ReadonlySet<string> =>
  new Set(diffConfigLeaves(draft, baseline, options).map((leaf) => leaf.path));

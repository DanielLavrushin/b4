import { B4SetConfig } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { isPlainObject } from "./common";

type Obj = Record<string, unknown>;

function stripObjectDefaults(obj: Obj, defaults: Obj): unknown {
  const result: Obj = {};
  for (const key of Object.keys(obj)) {
    if (!(key in defaults)) {
      result[key] = obj[key];
      continue;
    }
    const stripped = stripDefaults(obj[key], defaults[key]);
    if (stripped !== undefined) {
      result[key] = stripped;
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function stripDefaults(obj: unknown, defaults: unknown): unknown {
  if (Array.isArray(obj)) {
    return JSON.stringify(obj) === JSON.stringify(defaults) ? undefined : obj;
  }
  if (isPlainObject(obj) && isPlainObject(defaults)) {
    return stripObjectDefaults(obj, defaults);
  }
  return obj === defaults ? undefined : obj;
}

const FEATURE_OFF_RULES: Array<{
  path: string[];
  toggle: string;
  offValue: unknown;
  keep?: string[];
}> = [
  { path: ["faking"], toggle: "sni", offValue: false },
  { path: ["tcp", "duplicate"], toggle: "enabled", offValue: false },
  { path: ["tcp", "ip_block_detect"], toggle: "enabled", offValue: false },
  { path: ["tcp", "rst_protection"], toggle: "enabled", offValue: false },
  { path: ["tcp", "desync"], toggle: "mode", offValue: "off" },
  { path: ["tcp", "win"], toggle: "mode", offValue: "off" },
  { path: ["tcp", "incoming"], toggle: "mode", offValue: "off" },
  { path: ["fragmentation"], toggle: "strategy", offValue: "none" },
  { path: ["dns"], toggle: "enabled", offValue: false, keep: ["pins"] },
  { path: ["routing"], toggle: "enabled", offValue: false },
];

function resolveObjPath(root: Obj, path: string[]): Obj | undefined {
  let node: unknown = root;
  for (const seg of path) {
    if (!isPlainObject(node)) return undefined;
    node = node[seg];
  }
  return isPlainObject(node) ? node : undefined;
}

function setObjPath(root: Obj, path: string[], value: Obj): void {
  let node: Obj = root;
  for (let i = 0; i < path.length - 1; i++) {
    const next = node[path[i]];
    if (!isPlainObject(next)) return;
    node = next;
  }
  const lastKey = path.at(-1);
  if (lastKey !== undefined) node[lastKey] = value;
}

function resetDisabledFeatures(cfg: Obj, defaults: Obj): void {
  for (const rule of FEATURE_OFF_RULES) {
    const node = resolveObjPath(cfg, rule.path);
    const def = resolveObjPath(defaults, rule.path);
    if (!node || !def || node[rule.toggle] !== rule.offValue) continue;
    const reset: Obj = { ...def, [rule.toggle]: rule.offValue };
    for (const key of rule.keep ?? []) {
      if (node[key] !== undefined) reset[key] = node[key];
    }
    setObjPath(cfg, rule.path, reset);
  }
}

export function exportSetJson(config: B4SetConfig): string {
  const defaults = createDefaultSet(0);
  const alwaysInclude = new Set(["name", "enabled"]);
  const skip = new Set(["id", "stats", "hub_state", "revision"]);
  const configObj = structuredClone(config) as unknown as Record<
    string,
    unknown
  >;
  const defaultsObj = defaults as unknown as Record<string, unknown>;
  resetDisabledFeatures(configObj, defaultsObj);

  const result: Record<string, unknown> = {
    b4_version: import.meta.env.VITE_APP_VERSION || "dev",
  };

  for (const key of Object.keys(configObj)) {
    if (skip.has(key)) continue;
    if (alwaysInclude.has(key)) {
      result[key] = configObj[key];
      continue;
    }
    const stripped = stripDefaults(configObj[key], defaultsObj[key]);
    if (stripped !== undefined) {
      result[key] = stripped;
    }
  }

  if (isPlainObject(result.targets)) {
    delete result.targets.source_devices;
  }

  if (isPlainObject(result.discovery)) {
    const discovery = result.discovery;
    delete discovery.watchdog;
    if (Array.isArray(discovery.urls) && discovery.urls.length === 0) {
      delete discovery.urls;
    }
    if (Object.keys(discovery).length === 0) delete result.discovery;
  }

  delete result.escalate;

  return JSON.stringify(result);
}

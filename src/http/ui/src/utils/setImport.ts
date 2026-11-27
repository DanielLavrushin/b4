import { isPlainObject } from "./common";

type Obj = Record<string, unknown>;

const LEGACY_TCP_FIELDS = [
  ["desync_mode", "desync", "mode"],
  ["desync_ttl", "desync", "ttl"],
  ["desync_count", "desync", "count"],
  ["post_desync", "desync", "post_desync"],
  ["win_mode", "win", "mode"],
  ["win_values", "win", "values"],
] as const;

export function convertLegacySetShape(set: Obj): Obj {
  if (!isPlainObject(set.tcp)) return set;
  const tcp: Obj = { ...set.tcp };
  let converted = false;
  for (const [legacy, section, field] of LEGACY_TCP_FIELDS) {
    if (!(legacy in tcp)) continue;
    const current = tcp[section];
    tcp[section] = {
      [field]: tcp[legacy],
      ...(isPlainObject(current) ? current : {}),
    };
    delete tcp[legacy];
    converted = true;
  }
  return converted ? { ...set, tcp } : set;
}

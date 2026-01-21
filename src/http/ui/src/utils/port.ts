/**
 * Validates and normalizes port filter string.
 * Accepts single ports (1-65535) and ranges (min-max where 1 <= min < max <= 65535).
 * Returns comma-separated string with sorted unique ports and ranges.
 */
export function validatePortFilter(values: string[]): string {
  const valid: string[] = [];
  for (const v of values.map((s) => s.trim()).filter(Boolean)) {
    if (v.includes("-")) {
      const [s, e] = v.split("-").map((n) => parseInt(n.trim(), 10));
      if (!isNaN(s) && !isNaN(e) && s >= 1 && e >= 1 && s < e && e <= 65535) {
        valid.push(`${s}-${e}`);
      }
    } else {
      const p = parseInt(v, 10);
      if (!isNaN(p) && p >= 1 && p <= 65535) valid.push(p.toString());
    }
  }
  return [...new Set(valid)]
    .sort(
      (a, b) => parseInt(a.split("-")[0], 10) - parseInt(b.split("-")[0], 10),
    )
    .join(",");
}

/**
 * Parses port filter string into array of strings (ports and ranges).
 */
export function parsePortFilter(filter: string): string[] {
  if (!filter) return [];
  return filter
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean);
}

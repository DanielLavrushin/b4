/**
 * Validates and normalizes port filter string.
 * Accepts single ports (1-65535) and ranges (min-max where 1 <= min < max <= 65535).
 * Supports both "-" and ":" as range separators (normalized to "-").
 * Returns comma-separated string with sorted unique ports and ranges.
 */
export function validatePortFilter(values: string[]): string {
  const valid: string[] = [];
  for (const v of values.map((s) => s.trim()).filter(Boolean)) {
    // Normalize ":" to "-" for range separator
    const normalized = v.replace(":", "-");

    if (normalized.includes("-")) {
      const [s, e] = normalized
        .split("-")
        .map((n) => Number.parseInt(n.trim(), 10));
      if (
        !Number.isNaN(s) &&
        !Number.isNaN(e) &&
        s >= 1 &&
        e >= 1 &&
        s < e &&
        e <= 65535
      ) {
        valid.push(`${s}-${e}`);
      }
    } else {
      const p = Number.parseInt(normalized, 10);
      if (!Number.isNaN(p) && p >= 1 && p <= 65535) valid.push(p.toString());
    }
  }
  return [...new Set(valid)]
    .sort(
      (a, b) =>
        Number.parseInt(a.split("-")[0], 10) -
        Number.parseInt(b.split("-")[0], 10),
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

export interface ParsedConnectionFilter {
  fieldFilters: Record<string, string[]>;
  fieldExcludes: Record<string, string[]>;
  globalFilters: string[];
  globalExcludes: string[];
}

export function parseConnectionFilter(
  filter: string,
): ParsedConnectionFilter | null {
  const f = filter.trim().toLowerCase();
  if (!f) return null;

  const terms = f
    .split("+")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
  if (terms.length === 0) return null;

  const fieldFilters: Record<string, string[]> = {};
  const fieldExcludes: Record<string, string[]> = {};
  const globalFilters: string[] = [];
  const globalExcludes: string[] = [];

  for (const rawTerm of terms) {
    const isExclude = rawTerm.startsWith("!");
    const term = isExclude ? rawTerm.slice(1) : rawTerm;
    const colonIndex = term.indexOf(":");
    if (colonIndex > 0) {
      const field = term.substring(0, colonIndex);
      const value = term.substring(colonIndex + 1);
      const target = isExclude ? fieldExcludes : fieldFilters;
      (target[field] ??= []).push(value);
    } else if (isExclude) {
      globalExcludes.push(term);
    } else {
      globalFilters.push(term);
    }
  }

  return { fieldFilters, fieldExcludes, globalFilters, globalExcludes };
}

export function matchesConnectionFilter(
  parsed: ParsedConnectionFilter,
  getFieldValue: (field: string) => string,
  searchable: (string | null | undefined)[],
): boolean {
  for (const [field, values] of Object.entries(parsed.fieldFilters)) {
    const fieldValue = getFieldValue(field);
    if (!values.some((value) => fieldValue.includes(value))) return false;
  }

  for (const [field, values] of Object.entries(parsed.fieldExcludes)) {
    const fieldValue = getFieldValue(field);
    if (values.some((value) => fieldValue.includes(value))) return false;
  }

  for (const term of parsed.globalFilters) {
    if (!searchable.some((value) => value?.toLowerCase().includes(term)))
      return false;
  }

  for (const term of parsed.globalExcludes) {
    if (searchable.some((value) => value?.toLowerCase().includes(term)))
      return false;
  }

  return true;
}

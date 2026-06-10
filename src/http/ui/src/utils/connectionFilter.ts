export interface ParsedConnectionFilter {
  fieldFilters: Record<string, string[]>;
  fieldExcludes: Record<string, string[]>;
  globalFilters: string[];
  globalExcludes: string[];
}

function addTerm(acc: ParsedConnectionFilter, rawTerm: string): void {
  const isExclude = rawTerm.startsWith("!");
  const term = (isExclude ? rawTerm.slice(1) : rawTerm).trim();
  if (!term) return;

  const colonIndex = term.indexOf(":");
  if (colonIndex > 0) {
    const field = term.substring(0, colonIndex).trim();
    const value = term.substring(colonIndex + 1).trim();
    if (!field) return;
    const target = isExclude ? acc.fieldExcludes : acc.fieldFilters;
    target[field] ??= [];
    target[field].push(value);
    return;
  }

  if (isExclude) acc.globalExcludes.push(term);
  else acc.globalFilters.push(term);
}

function hasAnyTerm(acc: ParsedConnectionFilter): boolean {
  return (
    acc.globalFilters.length > 0 ||
    acc.globalExcludes.length > 0 ||
    Object.keys(acc.fieldFilters).length > 0 ||
    Object.keys(acc.fieldExcludes).length > 0
  );
}

export function parseConnectionFilter(
  filter: string,
): ParsedConnectionFilter | null {
  const f = filter.trim().toLowerCase();
  if (!f) return null;

  const acc: ParsedConnectionFilter = {
    fieldFilters: {},
    fieldExcludes: {},
    globalFilters: [],
    globalExcludes: [],
  };

  for (const rawTerm of f.split("+")) addTerm(acc, rawTerm);

  return hasAnyTerm(acc) ? acc : null;
}

function fieldMatches(fieldValue: string, value: string): boolean {
  return value ? fieldValue.includes(value) : fieldValue.trim() === "";
}

export function matchesConnectionFilter(
  parsed: ParsedConnectionFilter,
  getFieldValue: (field: string) => string,
  searchable: (string | null | undefined)[],
): boolean {
  for (const [field, values] of Object.entries(parsed.fieldFilters)) {
    const fieldValue = getFieldValue(field).toLowerCase();
    if (!values.some((value) => fieldMatches(fieldValue, value))) return false;
  }

  for (const [field, values] of Object.entries(parsed.fieldExcludes)) {
    const fieldValue = getFieldValue(field).toLowerCase();
    if (values.some((value) => fieldMatches(fieldValue, value))) return false;
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

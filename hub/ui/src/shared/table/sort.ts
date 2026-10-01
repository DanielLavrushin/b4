export type SortDir = "asc" | "desc";

export type SortValue = string | number | boolean | null | undefined;

const collators = new Map<string, Intl.Collator>();

const collator = (lang: string): Intl.Collator => {
  let c = collators.get(lang);
  if (!c) {
    c = new Intl.Collator(lang, { numeric: true, sensitivity: "base" });
    collators.set(lang, c);
  }
  return c;
};

export const compareValues = (a: SortValue, b: SortValue, lang: string): number => {
  const aEmpty = a === null || a === undefined || a === "";
  const bEmpty = b === null || b === undefined || b === "";
  if (aEmpty && bEmpty) return 0;
  if (aEmpty) return 1;
  if (bEmpty) return -1;
  if (typeof a === "number" && typeof b === "number") return a - b;
  if (typeof a === "boolean" && typeof b === "boolean") return Number(a) - Number(b);
  return collator(lang).compare(String(a), String(b));
};

export const sortRows = <T,>(rows: T[], value: (row: T) => SortValue, dir: SortDir, lang: string): T[] => {
  const indexed = rows.map((row, index) => ({ row, index, key: value(row) }));
  indexed.sort((x, y) => {
    const xEmpty = x.key === null || x.key === undefined || x.key === "";
    const yEmpty = y.key === null || y.key === undefined || y.key === "";
    if (xEmpty !== yEmpty) return xEmpty ? 1 : -1;
    const c = compareValues(x.key, y.key, lang);
    if (c !== 0) return dir === "asc" ? c : -c;
    return x.index - y.index;
  });
  return indexed.map((i) => i.row);
};

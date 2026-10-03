const MAX_ASN = 4294967295;

const RESERVED_ASNS: [number, number][] = [
  [0, 0],
  [23456, 23456],
  [64496, 131071],
  [4200000000, MAX_ASN],
];

export const asnNumber = (raw: string): string | null => {
  let value = raw.trim();
  if (/^asn/i.test(value)) value = value.slice(3);
  else if (/^as/i.test(value)) value = value.slice(2);
  value = value.trim();
  if (!/^\d+$/.test(value)) return null;
  const n = Number(value);
  return Number.isSafeInteger(n) && n <= MAX_ASN ? String(n) : null;
};

export const normalizeAsn = (raw: string): string | null => {
  const id = asnNumber(raw);
  if (id === null) return null;
  const n = Number(id);
  return RESERVED_ASNS.some(([lo, hi]) => n >= lo && n <= hi) ? null : id;
};

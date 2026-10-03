import type { MixView } from "@/models/api";

export interface OriginOption {
  key: string;
  head: string;
  detail: string;
  label: string;
  votes?: number;
  country?: string;
}

export interface OriginChoice {
  options: OriginOption[];
  value: OriginOption | null;
}

export const normalizeAsn = (value: string): string => {
  const v = value.trim().toUpperCase();
  return v.startsWith("AS") ? v.slice(2) : v;
};

export const normalizeCountry = (value: string): string => value.trim().toUpperCase();

const option = (key: string, head: string, detail: string, votes?: number, country?: string): OriginOption => ({
  key,
  head,
  detail,
  label: detail ? `${head} ${detail}` : head,
  votes,
  country,
});

export const asnOption = (m: MixView): OriginOption => option(m.key, `AS${m.key}`, m.name ?? m.country ?? "", m.votes, m.country);

export const asnFallback = (key: string): OriginOption => option(key, `AS${key}`, "");

export const regionNamer = (lang: string): ((code: string) => string) => {
  let names: Intl.DisplayNames | null;
  try {
    names = new Intl.DisplayNames([lang], { type: "region" });
  } catch {
    names = null;
  }
  return (code) => {
    if (!names) return "";
    try {
      const name = names.of(code);
      return name && name !== code ? name : "";
    } catch {
      return "";
    }
  };
};

export const countryOption = (m: MixView, regionName: (code: string) => string): OriginOption => option(m.key, m.key, regionName(m.key), m.votes);

export const countryFallback = (key: string, regionName: (code: string) => string): OriginOption => option(key, key, regionName(key));

export const choose = (options: OriginOption[], key: string, fallback: () => OriginOption): OriginChoice => {
  if (!key) return { options, value: null };
  const found = options.find((o) => o.key === key);
  if (found) return { options, value: found };
  const extra = fallback();
  return { options: [extra, ...options], value: extra };
};

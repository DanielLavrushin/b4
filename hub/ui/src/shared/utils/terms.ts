import type { TFunction } from "i18next";
import type { Term } from "@/models/api";

const extras: Record<string, [string, string][]> = {
  fake: [
    ["domain", "technique.fake.domain"],
    ["ttl", "technique.fake.ttl"],
    ["copies", "technique.fake.copies"],
  ],
  frag: [
    ["pool", "technique.frag.pool"],
    ["middle_sni", "technique.frag.middle"],
    ["reverse", "technique.frag.reverse"],
  ],
  desync: [["count", "technique.desync.count"]],
  dns: [
    ["host", "technique.dns.host"],
    ["strict", "technique.dns.strict"],
    ["fragment", "technique.dns.fragment"],
    ["pins", "technique.dns.pins"],
  ],
  block: [["action", "technique.block.action"]],
  udp: [["payload", "technique.udp.payloadLabel"]],
};

const params = (t: TFunction, term: Term): Record<string, unknown> => {
  const p: Record<string, unknown> = { ...(term.params ?? {}) };
  if (typeof p.payload === "string") {
    p.payloadLabel = t(`technique.payload.${p.payload}`, { defaultValue: p.payload });
  }
  return p;
};

export const techniqueChip = (t: TFunction, term: Term): string =>
  t(`technique.${term.code}.chip`, { ...params(t, term), defaultValue: term.code });

export const techniqueText = (t: TFunction, term: Term): string => {
  const p = params(t, term);
  const parts = [t(`technique.${term.code}.text`, { ...p, defaultValue: term.code })];
  (extras[term.code] ?? []).forEach(([field, key]) => {
    if (p[field] !== undefined && p[field] !== false && p[field] !== "") parts.push(t(key, p));
  });
  return parts.join(", ");
};

export const filterText = (t: TFunction, term: Term): string => t(`targetFilter.${term.code}`, { ...(term.params ?? {}), defaultValue: term.code });

export const flagText = (t: TFunction, flag: string): string => t(`flag.${flag}.label`, { defaultValue: flag });

export const flagHint = (t: TFunction, flag: string): string => t(`flag.${flag}.hint`, { defaultValue: "" });

interface TargetLists {
  domains: string[];
  ips: string[];
  geosite: string[];
  geoip: string[];
  asns: string[];
  filters: Term[];
}

const PREVIEW = 4;

const previewList = (t: TFunction, items: string[], label?: string): string => {
  if (items.length === 0) return "";
  const shown = items.slice(0, PREVIEW).join(", ");
  const more = items.length > PREVIEW ? ` ${t("targets.more", { count: items.length - PREVIEW })}` : "";
  return label ? `${label}: ${shown}${more}` : `${shown}${more}`;
};

export const targetsPreview = (t: TFunction, targets: TargetLists): string => {
  const parts = [
    previewList(t, targets.domains),
    previewList(t, targets.ips, t("targets.addresses")),
    previewList(t, targets.geosite, "geosite"),
    previewList(t, targets.geoip, "geoip"),
    previewList(t, targets.asns.map((a) => `AS${a}`), "ASN"),
  ].filter(Boolean);
  if (parts.length === 0) parts.push(t("targets.none"));
  targets.filters.forEach((f) => parts.push(filterText(t, f)));
  return parts.join("; ");
};

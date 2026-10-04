import type { TFunction } from "i18next";
import type { FacetSetConfig } from "@design";
import type { Term } from "@/models/api";
import { filterText } from "@/shared/utils/terms";

export const asSetConfig = (config?: Record<string, unknown>): FacetSetConfig | undefined =>
  config ? (config as unknown as FacetSetConfig) : undefined;

export interface TargetLists {
  domains: string[];
  domains_total?: number;
  ips: string[] | number;
  geosite: string[];
  geoip: string[];
  asns: string[];
}

export const targetSummaryOf = (t: TFunction, lists: TargetLists): string => {
  const ipList = Array.isArray(lists.ips) ? lists.ips : [];
  const ipCount = Array.isArray(lists.ips) ? lists.ips.length : lists.ips;
  const domainCount = Math.max(lists.domains_total ?? 0, lists.domains.length);
  const named = [...lists.geosite, ...lists.geoip, ...lists.asns.map((id) => `AS${id}`), ...lists.domains, ...ipList];
  const total = lists.geosite.length + lists.geoip.length + lists.asns.length + domainCount + ipCount;
  if (total === 0) return t("sets.card.noTargets");
  const ips = t("sets.card.ipCount", { count: ipCount });
  if (named.length === 0) return ips;
  const parts = [total > 1 ? `${named[0]} +${String(total - 1)}` : named[0]];
  if (domainCount > 0) parts.push(t("sets.card.domainCount", { count: domainCount }));
  if (ipCount > 0) parts.push(ips);
  return parts.join(" · ");
};

const PREVIEW = 4;

const preview = (t: TFunction, items: readonly string[], total = items.length): string => {
  const shown = items.slice(0, PREVIEW);
  const more = total - shown.length;
  return more > 0 ? `${shown.join(", ")} ${t("targets.more", { count: more })}` : shown.join(", ");
};

type PreviewLists = TargetLists & { filters: readonly Term[] };

const configLists = (config: FacetSetConfig, filters: readonly Term[]): PreviewLists => ({
  domains: config.targets.sni_domains,
  ips: config.targets.ip,
  geosite: config.targets.geosite_categories,
  geoip: config.targets.geoip_categories,
  asns: config.targets.asns ?? [],
  filters,
});

export const targetPreviewLines = (t: TFunction, brief: PreviewLists, config?: FacetSetConfig): string[] => {
  const lists = config ? configLists(config, brief.filters) : brief;
  const ipList = Array.isArray(lists.ips) ? lists.ips : [];
  const ipCount = Array.isArray(lists.ips) ? lists.ips.length : lists.ips;
  const domainCount = Math.max(lists.domains_total ?? 0, lists.domains.length);
  let ips = "";
  if (ipList.length > 0) ips = `${t("targets.addresses")}: ${preview(t, ipList)}`;
  else if (ipCount > 0) ips = t("sets.card.ipCount", { count: ipCount });
  return [
    domainCount > 0 ? preview(t, lists.domains, domainCount) : "",
    ips,
    lists.geosite.length > 0 ? `geosite: ${preview(t, lists.geosite)}` : "",
    lists.geoip.length > 0 ? `geoip: ${preview(t, lists.geoip)}` : "",
    lists.asns.length > 0 ? `ASN: ${preview(t, lists.asns.map((id) => `AS${id}`))}` : "",
    ...lists.filters.map((f) => filterText(t, f)),
  ].filter(Boolean);
};

import type { TFunction } from "i18next";
import type { FacetSetConfig } from "@design";

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
  const ips = `${ipCount.toLocaleString()} ${t("core.ips")}`;
  if (named.length === 0) return ips;
  const parts = [total > 1 ? `${named[0]} +${String(total - 1)}` : named[0]];
  if (domainCount > 0) parts.push(`${domainCount.toLocaleString()} ${t("core.domains")}`);
  if (ipCount > 0) parts.push(ips);
  return parts.join(" · ");
};

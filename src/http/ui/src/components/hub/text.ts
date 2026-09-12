import type { TFunction } from "i18next";
import {
  HubDisplayed,
  HubSet,
  HubTargets,
  HubWarning,
  formatWarningParam,
} from "@models/hub";

const PREVIEW_DOMAINS = 3;
const AUTHOR_PREVIEW = 12;

export function reportsText(t: TFunction, display: HubDisplayed): string {
  const count = Math.round(display.n);
  switch (display.bucket) {
    case "asn":
      return t("hub.card.reports.asn", { count });
    case "country":
      return t("hub.card.reports.country", { count });
    case "global":
      return t("hub.card.reports.global", { count });
    default:
      return t("hub.card.reports.none");
  }
}

export function scorePercent(display: HubDisplayed): number | null {
  if (display.bucket === "none") return null;
  return Math.round(Math.max(0, Math.min(1, display.score)) * 100);
}

export function targetsSummary(t: TFunction, targets: HubTargets): string {
  const parts: string[] = [];
  const domains = targets.domains ?? [];
  if (domains.length > 0) {
    const preview = domains.slice(0, PREVIEW_DOMAINS).join(", ");
    const rest = domains.length - PREVIEW_DOMAINS;
    parts.push(rest > 0 ? `${preview} +${rest}` : preview);
  }
  const categories = [...(targets.geosite ?? []), ...(targets.geoip ?? [])];
  if (categories.length > 0) {
    parts.push(t("hub.card.categories", { list: categories.join(", ") }));
  }
  if (domains.length > PREVIEW_DOMAINS) {
    parts.push(t("hub.card.domainCount", { count: domains.length }));
  }
  if (targets.ip_count > 0) {
    parts.push(t("hub.card.ipCount", { count: targets.ip_count }));
  }
  return parts.length > 0 ? parts.join(" · ") : t("hub.card.noTargets");
}

export function shortAuthor(author: string): string {
  return author.length > AUTHOR_PREVIEW
    ? `${author.slice(0, AUTHOR_PREVIEW)}…`
    : author;
}

export function flagLabel(t: TFunction, flag: string): string {
  return t(`hub.flags.${flag}`, { defaultValue: flag });
}

export function warningText(
  t: TFunction,
  prefix: string,
  warning: HubWarning,
): string {
  return t(`${prefix}.${warning.code}`, {
    defaultValue: warning.code,
    ...Object.fromEntries(
      Object.entries(warning.params ?? {}).map(([k, v]) => [
        k,
        formatWarningParam(v),
      ]),
    ),
  });
}

export function matchText(t: TFunction, set: HubSet): string {
  if (!set.match) return "";
  return set.match.via === "category"
    ? t("hub.card.match.category", { entry: set.match.entry })
    : t("hub.card.match.domain", { entry: set.match.entry });
}

export function defaultTestDomain(set: HubSet, searched: string): string {
  if (searched) return searched;
  const first = set.targets.domains?.[0] ?? "";
  return first.replace(/^\*\./, "");
}

export function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

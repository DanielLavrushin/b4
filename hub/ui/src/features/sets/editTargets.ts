import type { TFunction } from "i18next";
import type { FilterOptionsState } from "@mui/material";
import { ApiError } from "@/api/client";
import type { InvalidFieldView, Projection, WarningView } from "@/models/api";
import { normalizeAsn } from "@/shared/utils/asn";

export type ListKey = "sni_domains" | "ip" | "asns" | "geosite_categories" | "geoip_categories";
export type FieldKey = ListKey | "tls" | "ip_version" | "domain_only";

export const LIST_KEYS: ListKey[] = ["sni_domains", "ip", "asns", "geosite_categories", "geoip_categories"];
const FIELD_KEYS: FieldKey[] = [...LIST_KEYS, "tls", "ip_version", "domain_only"];

export const FIELD_LABELS: Record<FieldKey, string> = {
  sni_domains: "edit.domains",
  ip: "edit.ips",
  asns: "edit.asns",
  geosite_categories: "edit.geosite",
  geoip_categories: "edit.geoip",
  tls: "edit.tls",
  ip_version: "edit.ipVersion",
  domain_only: "edit.domainOnly",
};

export interface TargetsDraft {
  sni_domains: string;
  ip: string;
  asns: string;
  geosite_categories: string[];
  geoip_categories: string[];
  tls: string;
  ip_version: string;
  domain_only: boolean;
}

export const EMPTY_TARGETS: TargetsDraft = {
  sni_domains: "",
  ip: "",
  asns: "",
  geosite_categories: [],
  geoip_categories: [],
  tls: "",
  ip_version: "",
  domain_only: false,
};

export const TLS_OFFERED = ["", "1.2", "1.3"];
const TLS_KNOWN = new Set([...TLS_OFFERED, "1.0", "1.1"]);
export const IP_VERSIONS = ["", "4", "6"];

export const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const targetsOf = (projection: Projection): Record<string, unknown> =>
  isObject(projection.targets) ? projection.targets : {};

const stringsOf = (value: unknown): string[] => {
  const list: unknown[] = Array.isArray(value) ? value : [];
  return list.filter((item): item is string => typeof item === "string");
};

const scalar = (value: unknown): string => {
  if (typeof value === "string") return value;
  if (typeof value === "number") return String(value);
  return "";
};

const unique = (items: string[]): string[] => [...new Set(items)];

export const lines = (text: string): string[] =>
  text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");

export const tokens = (text: string): string[] => text.split(/[\s,]+/).filter((item) => item !== "");

const asnTokens = (text: string): string[] => tokens(text.replace(/\b(asn?)\s+(?=\d)/gi, "$1"));

export const asnList = (text: string): string[] => unique(asnTokens(text).map((item) => normalizeAsn(item) ?? item));

export const invalidAsns = (text: string): string[] => unique(asnTokens(text).filter((item) => normalizeAsn(item) === null));

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

const isIPv4 = (value: string): boolean => {
  const match = IPV4.exec(value);
  return match !== null && match.slice(1).every((part) => Number(part) <= 255 && (part.length === 1 || !part.startsWith("0")));
};

const isIPv6 = (value: string): boolean => {
  if (!value.includes(":") || !/^[0-9a-fA-F:.]+$/.test(value)) return false;
  try {
    return new URL(`http://[${value}]/`).hostname !== "";
  } catch {
    return false;
  }
};

export type AddressKind = "ok" | "invalid" | "catch_all";

export const classifyAddress = (raw: string): AddressKind => {
  const value = raw.trim();
  const slash = value.indexOf("/");
  if (slash < 0) return isIPv4(value) || isIPv6(value) ? "ok" : "invalid";
  const address = value.slice(0, slash);
  const bits = value.slice(slash + 1);
  if (!/^\d{1,3}$/.test(bits)) return "invalid";
  const prefix = Number(bits);
  let max = -1;
  if (isIPv4(address)) max = 32;
  else if (isIPv6(address)) max = 128;
  if (prefix > max) return "invalid";
  return prefix === 0 ? "catch_all" : "ok";
};

export const siteKey = (name: string): string => name.split("@")[0].trim().toLowerCase();

export const ipKey = (name: string): string => name.trim().toLowerCase();

export const normalizeCategories = (values: string[]): string[] => {
  const seen = new Set<string>();
  const out: string[] = [];
  values.flatMap(tokens).forEach((name) => {
    const key = name.toLowerCase();
    if (seen.has(key)) return;
    seen.add(key);
    out.push(name);
  });
  return out;
};

const OPTION_LIMIT = 100;

export const filterCategories = (options: string[], state: FilterOptionsState<string>): string[] => {
  const query = state.inputValue.trim().toLowerCase();
  if (query === "") return options.slice(0, OPTION_LIMIT);
  const exact: string[] = [];
  const prefix: string[] = [];
  const rest: string[] = [];
  options.forEach((option) => {
    if (option === query) exact.push(option);
    else if (option.startsWith(query)) prefix.push(option);
    else if (option.includes(query)) rest.push(option);
  });
  return [...exact, ...prefix, ...rest].slice(0, OPTION_LIMIT);
};

export const draftOf = (projection: Projection): TargetsDraft => {
  const targets = targetsOf(projection);
  return {
    sni_domains: stringsOf(targets.sni_domains).join("\n"),
    ip: stringsOf(targets.ip).join("\n"),
    asns: stringsOf(targets.asns).join("\n"),
    geosite_categories: stringsOf(targets.geosite_categories),
    geoip_categories: stringsOf(targets.geoip_categories),
    tls: scalar(targets.tls),
    ip_version: scalar(targets.ip_version),
    domain_only: targets.domain_only === true,
  };
};

const listOrNothing = (items: string[]): string[] | undefined => (items.length > 0 ? items : undefined);

const encoded = (draft: TargetsDraft, key: FieldKey): unknown => {
  switch (key) {
    case "sni_domains":
      return listOrNothing(lines(draft.sni_domains));
    case "ip":
      return listOrNothing(tokens(draft.ip));
    case "asns":
      return listOrNothing(asnList(draft.asns));
    case "geosite_categories":
      return listOrNothing(draft.geosite_categories);
    case "geoip_categories":
      return listOrNothing(draft.geoip_categories);
    case "tls":
      return draft.tls === "" ? undefined : draft.tls;
    case "ip_version":
      return draft.ip_version === "" ? undefined : draft.ip_version;
    case "domain_only":
      return draft.domain_only ? true : undefined;
  }
};

export const withTargets = (projection: Projection, draft: TargetsDraft, keys: FieldKey[]): Projection => {
  const current = targetsOf(projection);
  const values: Record<string, unknown> = {};
  keys.forEach((key) => {
    values[key] = encoded(draft, key);
  });
  const next: Record<string, unknown> = {};
  Object.entries(current).forEach(([key, value]) => {
    if (!(key in values)) next[key] = value;
    else if (values[key] !== undefined) next[key] = values[key];
  });
  Object.entries(values).forEach(([key, value]) => {
    if (!(key in current) && value !== undefined) next[key] = value;
  });
  const out: Projection = { ...projection };
  if (Object.keys(next).length > 0) out.targets = next;
  else delete out.targets;
  return out;
};

export const hasTargets = (projection: Projection): boolean => {
  const targets = targetsOf(projection);
  return LIST_KEYS.some((key) => stringsOf(targets[key]).length > 0);
};

export const sameField = (a: Projection, b: Projection, key: FieldKey): boolean =>
  JSON.stringify(targetsOf(a)[key]) === JSON.stringify(targetsOf(b)[key]);

export const tlsUnknown = (value: string): boolean => !TLS_KNOWN.has(value);

export const ipVersionUnknown = (value: string): boolean => !IP_VERSIONS.includes(value);

const WARNING_FIELDS: Record<string, FieldKey> = {
  private_addresses: "ip",
  invalid_addresses: "ip",
  catch_all_addresses: "ip",
  too_many_ips: "ip",
  geosite_categories_missing: "geosite_categories",
  geoip_categories_missing: "geoip_categories",
  too_many_asns: "asns",
  pin_not_targeted: "sni_domains",
  pin_private_address: "sni_domains",
  private_domains: "sni_domains",
  invalid_domains: "sni_domains",
  too_many_domains: "sni_domains",
  block_regexp: "sni_domains",
};

export const fieldOfPath = (path: string): FieldKey | null => {
  const match = /^targets\.([a-z_]+)/.exec(path);
  if (!match) return null;
  return FIELD_KEYS.find((key) => key === match[1]) ?? null;
};

export type FieldWarnings = Partial<Record<FieldKey, WarningView[]>>;

const push = <T>(map: Partial<Record<FieldKey, T[]>>, key: FieldKey, item: T) => {
  const list = map[key] ?? [];
  list.push(item);
  map[key] = list;
};

export const splitWarnings = (warnings: WarningView[]): { byField: FieldWarnings; rest: WarningView[] } => {
  const byField: FieldWarnings = {};
  const rest: WarningView[] = [];
  warnings.forEach((warning) => {
    const fields: unknown = warning.params?.fields;
    if (warning.code === "values_changed" && Array.isArray(fields)) {
      const list: unknown[] = fields;
      const perField: Partial<Record<FieldKey, unknown[]>> = {};
      const general: unknown[] = [];
      list.forEach((item) => {
        const key = isObject(item) && typeof item.path === "string" ? fieldOfPath(item.path) : null;
        if (key) push(perField, key, item);
        else general.push(item);
      });
      Object.entries(perField).forEach(([key, items]) => {
        const field = FIELD_KEYS.find((k) => k === key);
        if (field && items) push(byField, field, { code: warning.code, params: { ...warning.params, fields: items } });
      });
      if (general.length > 0) rest.push({ code: warning.code, params: { ...warning.params, fields: general } });
      return;
    }
    const key = WARNING_FIELDS[warning.code];
    if (key) push(byField, key, warning);
    else rest.push(warning);
  });
  return { byField, rest };
};

export interface KnownCategories {
  geosite: Set<string>;
  geoip: Set<string>;
}

const DOMAIN_SEPARATOR = /[\s,;]/;

const invalidDomains = (text: string): string[] =>
  unique(lines(text).filter((line) => !line.toLowerCase().startsWith("regexp:") && DOMAIN_SEPARATOR.test(line)));

export const clientWarnings = (draft: TargetsDraft, known: KnownCategories): FieldWarnings => {
  const out: FieldWarnings = {};
  const domains = invalidDomains(draft.sni_domains);
  if (domains.length > 0) push(out, "sni_domains", { code: "invalid_domains", params: { domains } });
  const addresses = tokens(draft.ip);
  const invalid = unique(addresses.filter((item) => classifyAddress(item) === "invalid"));
  const catchAll = unique(addresses.filter((item) => classifyAddress(item) === "catch_all"));
  if (invalid.length > 0) push(out, "ip", { code: "invalid_addresses", params: { addresses: invalid } });
  if (catchAll.length > 0) push(out, "ip", { code: "catch_all_addresses", params: { addresses: catchAll } });
  if (known.geosite.size > 0) {
    const missing = draft.geosite_categories.filter((name) => !known.geosite.has(siteKey(name)));
    if (missing.length > 0) push(out, "geosite_categories", { code: "geosite_categories_missing", params: { categories: missing } });
  }
  if (known.geoip.size > 0) {
    const missing = draft.geoip_categories.filter((name) => !known.geoip.has(ipKey(name)));
    if (missing.length > 0) push(out, "geoip_categories", { code: "geoip_categories_missing", params: { categories: missing } });
  }
  return out;
};

const isInvalidField = (value: unknown): value is InvalidFieldView =>
  isObject(value) && typeof value.path === "string" && typeof value.code === "string" && typeof value.message === "string";

export const invalidFields = (error: unknown): InvalidFieldView[] => {
  if (!(error instanceof ApiError) || error.code !== "invalid_set") return [];
  const fields: unknown = error.params.fields;
  if (!Array.isArray(fields)) return [];
  const list: unknown[] = fields;
  return list.filter(isInvalidField);
};

export const fieldErrorText = (t: TFunction, field: InvalidFieldView): string =>
  t(`edit.fieldErrors.${field.code}`, { ...field.params, defaultValue: field.message });

const show = (value: unknown): string => {
  if (typeof value === "string") return value;
  if (value === undefined) return "";
  return JSON.stringify(value);
};

const stringList = (value: unknown): string[] => {
  const list: unknown[] = Array.isArray(value) ? value : [];
  return list.map(show);
};

const INTERPOLATED = new Set(["count", "max", "size"]);

const changeText = (item: unknown): string =>
  isObject(item) ? `${show(item.path)}: ${show(item.from)} → ${show(item.to)}` : show(item);

export const warningItems = (warning: WarningView): string[] => {
  const p = warning.params ?? {};
  for (const key of ["addresses", "categories", "domains", "paths"]) {
    const value = p[key];
    if (Array.isArray(value)) return stringList(value);
  }
  const fields = p.fields;
  if (Array.isArray(fields)) {
    const list: unknown[] = fields;
    return list.map(changeText);
  }
  for (const key of ["domain", "entry", "host", "file", "path"]) {
    const value = p[key];
    if (typeof value === "string") return [value];
  }
  return Object.entries(p)
    .filter(([key]) => !INTERPOLATED.has(key))
    .map(([, value]) => show(value));
};

export const warningText = (t: TFunction, warning: WarningView): string =>
  t(`edit.warningCodes.${warning.code}`, { ...warning.params, defaultValue: warning.code });

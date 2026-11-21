import { exportSetJson } from "@utils";
import { B4SetConfig, SetTemplate } from "./config";
import { createDefaultSet } from "./defaults";

export const SET_TEMPLATE_OWN_KEYS = [
  "id",
  "name",
  "enabled",
  "targets",
  "hub",
  "discovery",
  "revision",
  "stats",
  "hub_state",
  "b4_version",
] as const;

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function overlay(base: unknown, patch: unknown): unknown {
  if (!isObj(base) || !isObj(patch)) return structuredClone(patch);
  const out: Obj = { ...base };
  for (const [key, value] of Object.entries(patch)) {
    out[key] = overlay(out[key], value);
  }
  return out;
}

function withoutOwnKeys(template: Obj): Obj {
  const fields = { ...template };
  for (const key of SET_TEMPLATE_OWN_KEYS) delete fields[key];
  return fields;
}

export function createSetFromTemplate(
  setCount: number,
  template?: SetTemplate | null,
): B4SetConfig {
  const set = createDefaultSet(setCount);
  if (!template) return set;
  return overlay(set, withoutOwnKeys(template)) as B4SetConfig;
}

function withoutProxyLogin(fields: Obj): Obj {
  const routing = fields.routing;
  if (!isObj(routing) || !isObj(routing.upstream)) return fields;
  const { username: _username, password: _password, ...upstream } =
    routing.upstream;
  const nextRouting: Obj = { ...routing, upstream };
  if (Object.keys(upstream).length === 0) delete nextRouting.upstream;
  const next: Obj = { ...fields, routing: nextRouting };
  if (Object.keys(nextRouting).length === 0) delete next.routing;
  return next;
}

export function setTemplateFromSet(set: B4SetConfig): SetTemplate {
  return withoutProxyLogin(
    withoutOwnKeys(JSON.parse(exportSetJson(set)) as Obj),
  );
}

export function formatSetTemplate(template?: SetTemplate | null): string {
  if (!template || Object.keys(template).length === 0) return "";
  return JSON.stringify(template, null, 2);
}

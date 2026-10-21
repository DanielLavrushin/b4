import i18n from "../i18n";
import type { TFunction } from "i18next";
import { ApiError, type FieldError } from "@api/apiClient";

const SET_PATH_PREFIX = /^sets\[\d+\]\./;

const SET_FIELD_ERRORS: Record<string, string> = {
  "udp.dport_filter:required": "udp_l7_ports",
  "udp.filter_l7:conflict": "udp_l7_targets",
  "tcp.dport_filter:conflict": "udp_l7_tcp_ports",
  "routing.source_interfaces:conflict": "udp_l7_source_interfaces",
};

function fieldErrorKey(f: FieldError): string {
  const path = f.path.replace(SET_PATH_PREFIX, "");
  return `errors.${SET_FIELD_ERRORS[`${path}:${f.code}`] ?? f.code}`;
}

export function localizeFieldError(f: FieldError, lng?: string): string {
  const key = fieldErrorKey(f);
  if (i18n.exists(key, { lng })) {
    const out: unknown = i18n.t(key, { ...(f.params ?? {}), lng });
    if (typeof out === "string") return out;
  }
  return f.message;
}

export function describeApiError(error: unknown): string {
  if (error instanceof ApiError && error.fields && error.fields.length > 0) {
    return error.fields.map((f) => localizeFieldError(f)).join("; ");
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error);
}

function apiErrorParams(
  error: ApiError,
  extra?: Record<string, unknown>,
): Record<string, unknown> {
  const body =
    error.body && typeof error.body === "object"
      ? (error.body as Record<string, unknown>)
      : {};
  const params: Record<string, unknown> = {
    detail: typeof body.error === "string" ? body.error : error.message,
  };
  if (Array.isArray(body.used_by)) {
    params.sets = body.used_by
      .filter((name): name is string => typeof name === "string")
      .join(", ");
  }
  return { ...params, ...extra };
}

export function localizeApiError(
  error: unknown,
  extra?: Record<string, unknown>,
): string {
  if (error instanceof ApiError) {
    if (error.fields && error.fields.length > 0) {
      return error.fields.map((f) => localizeFieldError(f)).join("; ");
    }
    if (error.code) {
      const key = `errors.${error.code}`;
      if (i18n.exists(key)) {
        const out: unknown = i18n.t(key, apiErrorParams(error, extra));
        if (typeof out === "string") return out;
      }
    }
  }
  return describeApiError(error);
}

interface HubErrorBody {
  code?: string;
  retry_after?: number;
  scope?: string;
  limit?: number;
  window?: string;
}

const hubScopes = new Set(["share", "vote", "report", "mirror", "newkey", "request"]);

function formatRetry(t: TFunction, seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.ceil((seconds % 3600) / 60);
  if (hours > 0 && minutes > 0) return t("hub.errors.retry.hoursMinutes", { hours, minutes });
  if (hours > 0) return t("hub.errors.retry.hours", { count: hours });
  if (minutes > 0) return t("hub.errors.retry.minutes", { count: minutes });
  return t("hub.errors.retry.moment");
}

export function describeHubError(error: unknown, t: TFunction): string {
  if (error instanceof ApiError && error.body && typeof error.body === "object") {
    const body = error.body as HubErrorBody;
    if (error.code === "rate_limited") {
      const scope = body.scope && hubScopes.has(body.scope) ? body.scope : "request";
      const window = body.window === "hour" ? "hour" : "day";
      return t("hub.errors.rateLimited", {
        what: t(`hub.errors.scope.${scope}`, { count: body.limit ?? 0 }),
        window: t(`hub.errors.window.${window}`),
        retry: formatRetry(t, body.retry_after ?? 0),
      });
    }
    if (error.code === "banned") return t("hub.errors.banned");
  }
  return describeApiError(error);
}

const staleWriteCodes = new Set(["config_changed", "set_changed"]);

export function isStaleWriteError(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.status === 409 &&
    !!error.code &&
    staleWriteCodes.has(error.code)
  );
}

export function reportStaleWrite(
  error: ApiError,
  showSnackbar: (
    message: string,
    severity: "error",
    action: { label: string; onClick: () => void },
  ) => void,
  t: TFunction,
  onReload: () => void,
): void {
  showSnackbar(t(`errors.${error.code ?? "config_changed"}`), "error", {
    label: t("core.reload"),
    onClick: onReload,
  });
}

export function describeCodedError(
  error: unknown,
  t: TFunction,
  prefix: string,
): string {
  if (error instanceof ApiError && error.code) {
    const key = `${prefix}.${error.code}`;
    if (i18n.exists(key)) return t(key);
  }
  return describeApiError(error);
}

export function reportSaveError(
  error: unknown,
  showError: (message: string) => void,
  t: TFunction,
  fallbackKey = "core.configSaveError",
): void {
  if (error instanceof ApiError && error.fields && error.fields.length > 0) {
    for (const f of error.fields) showError(localizeFieldError(f));
    return;
  }
  if (error instanceof Error && error.message) {
    showError(error.message);
    return;
  }
  showError(t(fallbackKey));
}

import i18n from "../i18n";
import type { TFunction } from "i18next";
import { ApiError, type FieldError } from "@api/apiClient";

export function localizeFieldError(f: FieldError, lng?: string): string {
  const key = `errors.${f.code}`;
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

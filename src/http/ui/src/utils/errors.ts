import i18n from "../i18n";
import type { TFunction } from "i18next";
import { ApiError, type FieldError } from "@api/apiClient";

export function localizeFieldError(f: FieldError): string {
  const key = `errors.${f.code}`;
  if (i18n.exists(key)) {
    const out: unknown = i18n.t(key, f.params ?? {});
    if (typeof out === "string") return out;
  }
  return f.message;
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

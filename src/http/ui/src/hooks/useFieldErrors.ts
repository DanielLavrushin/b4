import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, FieldError } from "@api/apiClient";
import { localizeFieldError } from "../utils/errors";

export type LocalizedFieldError = FieldError & { localizedMessage: string };
export type FieldErrorMap = Record<string, LocalizedFieldError>;

export function useFieldErrors(error: unknown): FieldErrorMap {
  const { i18n } = useTranslation();
  const language = i18n.language;
  return useMemo(() => {
    if (!(error instanceof ApiError) || !error.fields) return {};
    const map: FieldErrorMap = {};
    for (const f of error.fields) {
      map[f.path] = { ...f, localizedMessage: localizeFieldError(f, language) };
    }
    return map;
  }, [error, language]);
}

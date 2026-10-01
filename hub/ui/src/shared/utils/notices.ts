import type { TFunction } from "i18next";
import { ApiError } from "@/api/client";

export interface Coded {
  notice?: string;
  code?: string;
  params?: Record<string, unknown>;
}

export const noticeText = (t: TFunction, result: Coded): string => {
  if (!result.code) return result.notice ?? "";
  return t(`notices.${result.code}`, { ...(result.params ?? {}), defaultValue: result.notice ?? result.code });
};

export const errorText = (t: TFunction, error: unknown): string => {
  if (error instanceof ApiError) {
    return t(`errors.${error.code}`, { ...error.params, message: error.message, defaultValue: error.message });
  }
  if (error instanceof Error) return error.message;
  return String(error);
};

export const errorCode = (error: unknown): string => (error instanceof ApiError ? error.code : "");

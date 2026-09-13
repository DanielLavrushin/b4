import type { TFunction } from "i18next";

export const parseTime = (value?: string | null): Date | null => {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return null;
  return date;
};

export const formatStamp = (value?: string | null): string => {
  const date = parseTime(value);
  if (!date) return "";
  return date.toISOString().slice(0, 16).replace("T", " ") + " UTC";
};

export const formatDay = (value?: string | null): string => {
  if (!value) return "";
  return value.length >= 10 ? value.slice(0, 10) : value;
};

export const formatAgo = (t: TFunction, value?: string | null, now = Date.now()): string => {
  const date = parseTime(value);
  if (!date) return t("app.never");
  const seconds = Math.max(0, Math.floor((now - date.getTime()) / 1000));
  if (seconds < 60) return t("time.justNow");
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return t("time.minutes", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return t("time.hours", { count: hours });
  return t("time.days", { count: Math.floor(hours / 24) });
};

export const formatBytes = (size?: number): string => {
  if (!size) return "";
  if (size < 1024) return `${String(size)} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
};

export const shortHash = (value: string, length = 16): string =>
  value.length > length ? value.slice(0, length) : value;

export const setRef = (id: string, version: number): string =>
  `${id}/${String(version)}`;

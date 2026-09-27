import type { TFunction } from "i18next";

const legacyDefaults = new Set(["hidden by moderator", "banned by moderator"]);

export const reasonText = (t: TFunction, raw: string | undefined, independent?: number): string => {
  const value = (raw ?? "").trim();
  if (value === "" || legacyDefaults.has(value)) return t("reason.none");
  if (value === "reports") return t("reason.reports", { count: independent ?? 3 });
  return value;
};

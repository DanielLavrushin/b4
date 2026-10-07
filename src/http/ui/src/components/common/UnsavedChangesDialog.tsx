import type { ReactNode } from "react";
import {
  Box,
  Button,
  DialogContent,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Dialog } from "./B4Dialog";
import { colors } from "@design";

export interface UnsavedChangeItem {
  path: string;
  label: ReactNode;
  before: unknown;
  after: unknown;
}

export interface UnsavedChangeGroup {
  key: string;
  label: string;
  items: UnsavedChangeItem[];
}

export interface ChangeGroupDef {
  prefixes: string[];
  labelKey: string;
}
const humanizeSegment = (segment: string): string =>
  segment.replaceAll("_", " ");

export const groupChangeItems = (
  items: UnsavedChangeItem[],
  defs: ChangeGroupDef[],
): UnsavedChangeGroup[] => {
  const groups = new Map<string, UnsavedChangeGroup>();
  for (const item of items) {
    const match = defs
      .filter((def) =>
        def.prefixes.some(
          (prefix) => item.path === prefix || item.path.startsWith(`${prefix}.`),
        ),
      )
      .sort((a, b) => {
        const longest = (def: ChangeGroupDef) =>
          Math.max(...def.prefixes.map((prefix) => prefix.length));
        return longest(b) - longest(a);
      })[0];
    const key = match ? match.labelKey : item.path.split(".")[0];
    const label = match ? match.labelKey : humanizeSegment(key);
    const group = groups.get(key) ?? { key, label, items: [] };
    group.items.push(item);
    groups.set(key, group);
  }
  return [...groups.values()];
};

const SENSITIVE_PATH = /token|secret|password/i;

export function UnsavedChangesDialog({
  open,
  groups,
  total,
  onStay,
  onLeave,
}: Readonly<{
  open: boolean;
  groups: UnsavedChangeGroup[];
  total: number;
  onStay: () => void;
  onLeave: () => void;
}>) {
  const { t } = useTranslation();

  const formatValue = (value: unknown, path: string): string => {
    if (SENSITIVE_PATH.test(path)) return "••••••";
    if (value === undefined || value === null || value === "") return "—";
    if (typeof value === "boolean")
      return value ? t("core.valueOn") : t("core.valueOff");
    if (Array.isArray(value))
      return value.length === 0
        ? "—"
        : value.map((entry) => String(entry)).join(", ");
    if (typeof value === "string" || typeof value === "number")
      return String(value);
    try {
      return JSON.stringify(value) ?? "—";
    } catch {
      return "—";
    }
  };

  return (
    <B4Dialog
      title={t("core.unsavedChangesTitle")}
      open={open}
      onClose={onStay}
      actions={
        <>
          <Button onClick={onStay}>{t("core.unsavedStay")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button onClick={onLeave} variant="contained" color="warning">
            {t("core.unsavedLeave")}
          </Button>
        </>
      }
    >
      <DialogContent>
        <Typography variant="body2" sx={{ color: colors.text.secondary, mb: 2 }}>
          {t("core.unsavedChangesBody", { count: total })}
        </Typography>
        <Stack spacing={2}>
          {groups.map((group) => (
            <Box key={group.key}>
              <Typography
                variant="subtitle2"
                sx={{ color: colors.secondary, mb: 0.5 }}
              >
                {group.label} · {group.items.length}
              </Typography>
              <Stack spacing={0.5}>
                {group.items.map((item) => (
                  <Box key={item.path}>
                    <Typography variant="body2">{item.label}</Typography>
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary, display: "block" }}
                    >
                      {item.path}
                    </Typography>
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary, display: "block" }}
                    >
                      {formatValue(item.before, item.path)}
                      {" → "}
                      {formatValue(item.after, item.path)}
                    </Typography>
                  </Box>
                ))}
              </Stack>
            </Box>
          ))}
        </Stack>
      </DialogContent>
    </B4Dialog>
  );
}

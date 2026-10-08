import type { ReactNode } from "react";
import type { ChangedLeaf } from "@utils";
import {
  Box,
  Button,
  DialogContent,
  Stack,
  Tooltip,
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

export function buildChangeItems(
  leaves: ChangedLeaf[],
  labels: ReadonlyMap<string, ReactNode>,
): UnsavedChangeItem[] {
  return leaves.flatMap((leaf) => {
    const parent = {
      path: leaf.path,
      label: labels.get(leaf.path) ?? leaf.path,
      before: leaf.before,
      after: leaf.after,
    };
    if (!Array.isArray(leaf.before) && !Array.isArray(leaf.after)) {
      return [parent];
    }
    const before = new Set(
      (Array.isArray(leaf.before) ? leaf.before : []).map((v) => String(v)),
    );
    const after = new Set(
      (Array.isArray(leaf.after) ? leaf.after : []).map((v) => String(v)),
    );
    const rows = [...new Set([...before, ...after])]
      .filter((el) => labels.has(`${leaf.path}.${el}`))
      .filter((el) => before.has(el) !== after.has(el))
      .map((el) => ({
        path: `${leaf.path}.${el}`,
        label: labels.get(`${leaf.path}.${el}`) ?? el,
        before: before.has(el),
        after: after.has(el),
      }));
    return rows.length > 0 ? rows : [parent];
  });
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
  title,
  body,
  stayLabel,
  leaveLabel,
}: Readonly<{
  open: boolean;
  groups: UnsavedChangeGroup[];
  total: number;
  onStay: () => void;
  onLeave: () => void;
  title?: string;
  body?: string;
  stayLabel?: string;
  leaveLabel?: string;
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
      title={title ?? t("core.unsavedChangesTitle")}
      open={open}
      onClose={onStay}
      actions={
        <>
          <Button onClick={onStay}>{stayLabel ?? t("core.unsavedStay")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button onClick={onLeave} variant="contained" color="warning">
            {leaveLabel ?? t("core.unsavedLeave")}
          </Button>
        </>
      }
    >
      <DialogContent>
        <Typography variant="body2" sx={{ color: colors.text.secondary, mb: 2 }}>
          {body ?? t("core.unsavedChangesBody", { count: total })}
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
                    {typeof item.label === "string" && item.label !== item.path ? (
                      <Tooltip title={item.path} placement="top-start">
                        <Typography variant="body2" sx={{ width: "fit-content" }}>
                          {item.label}
                        </Typography>
                      </Tooltip>
                    ) : (
                      <Typography variant="body2">{item.label}</Typography>
                    )}
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

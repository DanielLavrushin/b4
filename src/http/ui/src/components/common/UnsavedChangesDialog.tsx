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
  groupPrefix?: string;
  subLabel?: string;
}

export function buildChangeItems(
  leaves: ChangedLeaf[],
  labels: ReadonlyMap<string, ReactNode>,
  valueLabels?: ReadonlyMap<string, ReadonlyMap<string, string>>,
): UnsavedChangeItem[] {
  return leaves.flatMap((leaf) => {
    const resolve = (value: unknown): unknown => {
      if (typeof value !== "string" && typeof value !== "number") return value;
      return valueLabels?.get(leaf.path)?.get(String(value)) ?? value;
    };
    const parent = {
      path: leaf.path,
      label:
        labels.get(leaf.path) ??
        capitalize(humanizeSegment(leaf.path.split(".").pop() ?? leaf.path)),
      before: resolve(leaf.before),
      after: resolve(leaf.after),
    };
    if (!Array.isArray(leaf.before) && !Array.isArray(leaf.after)) {
      return [parent];
    }
    const keyOf = (v: unknown): string =>
      typeof v === "object" && v !== null ? (JSON.stringify(v) ?? "") : String(v);
    const beforeMap = new Map(
      (Array.isArray(leaf.before) ? leaf.before : []).map(
        (v) => [keyOf(v), v] as const,
      ),
    );
    const afterMap = new Map(
      (Array.isArray(leaf.after) ? leaf.after : []).map(
        (v) => [keyOf(v), v] as const,
      ),
    );
    const added = [...afterMap].filter(([k]) => !beforeMap.has(k)).map(([, v]) => resolve(v));
    const removed = [...beforeMap].filter(([k]) => !afterMap.has(k)).map(([, v]) => resolve(v));
    if (added.length === 0 && removed.length === 0) return [parent];
    return [
      {
        path: leaf.path,
        label: parent.label,
        before: removed,
        after: added,
      },
    ];
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
const capitalize = (text: string): string =>
  text.length > 0 ? text[0].toUpperCase() + text.slice(1) : text;

export const groupChangeItems = (
  items: UnsavedChangeItem[],
  defs: ChangeGroupDef[],
  subDefs?: { label: string; prefixes: string[] }[],
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
    const label = match ? match.labelKey : capitalize(humanizeSegment(key));
    const matchedPrefix = match
      ? match.prefixes
          .filter(
            (prefix) => item.path === prefix || item.path.startsWith(`${prefix}.`),
          )
          .sort((a, b) => b.length - a.length)[0]
      : undefined;
    const group = groups.get(key) ?? { key, label, items: [] };
    let subLabel: string | undefined;
    if (subDefs) {
      const sub = subDefs
        .filter((def) =>
          def.prefixes.some(
            (prefix) => item.path === prefix || item.path.startsWith(`${prefix}.`),
          ),
        )
        .sort((a, b) => {
          const longest = Math.max(...a.prefixes.map((p) => p.length));
          const longestB = Math.max(...b.prefixes.map((p) => p.length));
          return longestB - longest;
        })[0];
      subLabel = sub?.label;
    }
    group.items.push({ ...item, groupPrefix: matchedPrefix, subLabel });
    groups.set(key, group);
  }
  const order = new Map(defs.map((def, index) => [def.labelKey, index]));
  return [...groups.values()].sort(
    (a, b) => (order.get(a.key) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.key) ?? Number.MAX_SAFE_INTEGER),
  );
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
  const subgroupOf = (item: UnsavedChangeItem): string[] => {
    const prefix = item.groupPrefix;
    const rest = prefix
      ? item.path === prefix
        ? ""
        : item.path.slice(prefix.length + 1)
      : item.path;
    return rest ? rest.split(".").slice(0, -1) : [];
  };
  const subgroupLabel = (segments: string[]): string =>
    segments.map((s) => capitalize(humanizeSegment(s))).join(" / ");
  const formatEntry = (entry: unknown): string => {
    if (typeof entry === "string" || typeof entry === "number") return String(entry);
    if (entry !== null && typeof entry === "object") {
      const record = entry as Record<string, unknown>;
      const name = record["name"] ?? record["mac"] ?? record["id"];
      if (typeof name === "string" && name) return name;
    }
    try {
      return JSON.stringify(entry) ?? "—";
    } catch {
      return "—";
    }
  };
  const formatValue = (value: unknown, path: string): string => {
    if (SENSITIVE_PATH.test(path)) return "••••••";
    if (value === undefined || value === null || value === "") return "—";
    if (typeof value === "boolean")
      return value ? t("core.valueOn") : t("core.valueOff");
    if (Array.isArray(value))
      return value.length === 0 ? "—" : value.map(formatEntry).join(", ");
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
                {(() => {
                  const subgroups = new Map<string, typeof group.items>();
                  for (const item of group.items) {
                    const key = item.subLabel ?? subgroupOf(item).join(".");
                    const list = subgroups.get(key) ?? [];
                    list.push(item);
                    subgroups.set(key, list);
                  }
                  return [...subgroups].map(([key, items]) => (
                    <Box key={key || "root"} sx={key ? { pl: 1.5 } : undefined}>
                      {key && key !== group.label && (
                        <Typography
                          variant="caption"
                          sx={{ color: colors.text.secondary, fontWeight: 600, display: "block", mb: 0.5, ml: -1.5 }}
                        >
                          {key}
                        </Typography>
                      )}
                      {items.map((item) => (
                  <Box key={item.path}>
                    {typeof item.label === "string" &&
                    item.label !== subgroupOf(item).join(".") &&
                    item.label !== group.label &&
                    item.path !== item.groupPrefix ? (
                      <Tooltip title={subgroupLabel(subgroupOf(item))} placement="top-start">
                        <Typography variant="body2" sx={{ width: "fit-content" }}>
                          {item.label}
                        </Typography>
                      </Tooltip>
                    ) : null}
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary, display: "block" }}
                    >
                      {Array.isArray(item.before) || Array.isArray(item.after) ? (
                        <>
                          {(item.after as unknown[]).length > 0 && (
                            <>{t("core.addedItems")}: {formatValue(item.after, item.path)}</>
                          )}
                          {(item.after as unknown[]).length > 0 &&
                            (item.before as unknown[]).length > 0 && <br />}
                          {(item.before as unknown[]).length > 0 && (
                            <>{t("core.removedItems")}: {formatValue(item.before, item.path)}</>
                          )}
                        </>
                      ) : (
                        <>
                          {formatValue(item.before, item.path)}
                          {" → "}
                          {formatValue(item.after, item.path)}
                        </>
                      )}
                    </Typography>
                  </Box>
                      ))}
                    </Box>
                  ));
                })()}
              </Stack>
            </Box>
          ))}
        </Stack>
      </DialogContent>
    </B4Dialog>
  );
}

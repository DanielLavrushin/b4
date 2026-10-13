import { memo, useMemo } from "react";
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
  blockLabel?: string;
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

const matchPrefix = (prefixes: string[], path: string): string | undefined => {
  let best: string | undefined;
  for (const prefix of prefixes) {
    if (path === prefix || path.startsWith(`${prefix}.`)) {
      if (!best || prefix.length > best.length) best = prefix;
    }
  }
  return best;
};

export interface SubDef {
  label: string;
  prefixes: string[];
  blocks?: { prefixes: string[]; label: string }[];
}

export const groupChangeItems = (
  items: UnsavedChangeItem[],
  defs: ChangeGroupDef[],
  subDefs?: SubDef[],
): UnsavedChangeGroup[] => {
  // Winner = longest *matched* prefix; first wins on ties. Ranking by the
  // def's longest prefix overall would let a sibling prefix win the match.
  const groups = new Map<string, UnsavedChangeGroup>();
  for (const item of items) {
    let match: ChangeGroupDef | undefined;
    let matchLen = -1;
    for (const def of defs) {
      const hit = matchPrefix(def.prefixes, item.path);
      if (hit && hit.length > matchLen) {
        match = def;
        matchLen = hit.length;
      }
    }
    const key = match ? match.labelKey : item.path.split(".")[0];
    const label = match ? match.labelKey : capitalize(humanizeSegment(key));
    const matchedPrefix = match
      ? matchPrefix(match.prefixes, item.path)
      : undefined;
    const group = groups.get(key) ?? { key, label, items: [] };
    let subLabel: string | undefined;
    let subMatch: SubDef | undefined;
    if (subDefs) {
      let subMatchLen = -1;
      for (const def of subDefs) {
        const hit = matchPrefix(def.prefixes, item.path);
        if (hit && hit.length > subMatchLen) {
          subMatch = def;
          subMatchLen = hit.length;
        }
      }
      subLabel = subMatch?.label;
    }
    let blockLabel: string | undefined;
    if (subMatch?.blocks) {
      let blockLen = -1;
      for (const block of subMatch.blocks) {
        const hit = matchPrefix(block.prefixes, item.path);
        if (hit && hit.length > blockLen) {
          blockLen = hit.length;
          blockLabel = block.label;
        }
      }
    }
    group.items.push({ ...item, groupPrefix: matchedPrefix, subLabel, blockLabel });
    groups.set(key, group);
  }
  const order = new Map(defs.map((def, index) => [def.labelKey, index]));
  return [...groups.values()].sort(
    (a, b) => (order.get(a.key) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.key) ?? Number.MAX_SAFE_INTEGER),
  );
};
const SENSITIVE_PATH = /token|secret|password/i;

const subgroupOf = (item: UnsavedChangeItem): string[] => {
  const prefix = item.groupPrefix;
  const rest = prefix
    ? item.path === prefix
      ? ""
      : item.path.slice(prefix.length + 1)
    : item.path;
  return rest ? rest.split(".").slice(0, -1) : [];
};
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
const formatValue = (
  value: unknown,
  path: string,
  t: (key: string) => string,
): string => {
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

const ChangeItemView = memo(function ChangeItemView({
  item,
  groupLabel,
  t,
}: Readonly<{
  item: UnsavedChangeItem;
  groupLabel: string;
  t: (key: string) => string;
}>) {
  return (
    <Box>
      {typeof item.label === "string" &&
      item.label !== subgroupOf(item).join(".") &&
      item.label !== groupLabel &&
      item.label !== item.blockLabel &&
      item.path !== item.groupPrefix ? (
        <Tooltip title={item.path} placement="top-start">
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
              <>{t("core.addedItems")}: {formatValue(item.after, item.path, t)}</>
            )}
            {(item.after as unknown[]).length > 0 &&
              (item.before as unknown[]).length > 0 && <br />}
            {(item.before as unknown[]).length > 0 && (
              <>{t("core.removedItems")}: {formatValue(item.before, item.path, t)}</>
            )}
          </>
        ) : (
          <>
            {formatValue(item.before, item.path, t)}
            {" → "}
            {formatValue(item.after, item.path, t)}
          </>
        )}
      </Typography>
    </Box>
  );
});

const SubgroupView = memo(function SubgroupView({
  subgroupKey,
  groupLabel,
  items,
  t,
}: Readonly<{
  subgroupKey: string;
  groupLabel: string;
  items: UnsavedChangeItem[];
  t: (key: string) => string;
}>) {
  const [unblocked, blocks] = useMemo(() => {
    const plain: UnsavedChangeItem[] = [];
    const map = new Map<string, UnsavedChangeItem[]>();
    for (const item of items) {
      if (!item.blockLabel) {
        plain.push(item);
        continue;
      }
      const list = map.get(item.blockLabel) ?? [];
      list.push(item);
      map.set(item.blockLabel, list);
    }
    return [plain, [...map]] as const;
  }, [items]);
  return (
    <Box sx={subgroupKey ? { pl: 1.5 } : undefined}>
      {subgroupKey && subgroupKey !== groupLabel && (
        <Typography
          variant="caption"
          sx={{ color: colors.text.secondary, fontWeight: 600, display: "block", mb: 0.5, ml: -1.5 }}
        >
          {subgroupKey}
        </Typography>
      )}
      {unblocked.map((item) => (
        <ChangeItemView key={item.path} item={item} groupLabel={groupLabel} t={t} />
      ))}
      {blocks.map(([blockLabel, blockItems]) => {
        // Block header repeating the subgroup/group header adds no info:
        // render such items flat at the subgroup level.
        if (blockLabel === subgroupKey || blockLabel === groupLabel) {
          return blockItems.map((item) => (
            <ChangeItemView key={item.path} item={item} groupLabel={groupLabel} t={t} />
          ));
        }
        return (
          <Box key={blockLabel} sx={{ pl: 1.5 }}>
            <Typography
              variant="caption"
              sx={{ color: colors.text.secondary, fontWeight: 600, display: "block", mb: 0.5, ml: -1.5 }}
            >
              {blockLabel}
            </Typography>
            {blockItems.map((item) => (
              // Label hidden as a block-header duplicate: align the value
              // with the header instead of indenting it under nothing.
              <Box key={item.path} sx={item.label === blockLabel ? { ml: -1.5 } : undefined}>
                <ChangeItemView item={item} groupLabel={groupLabel} t={t} />
              </Box>
            ))}
          </Box>
        );
      })}
    </Box>
  );
});

const ChangeGroupView = memo(function ChangeGroupView({
  group,
  t,
}: Readonly<{ group: UnsavedChangeGroup; t: (key: string) => string }>) {
  const subgroups = useMemo(() => {
    const map = new Map<string, typeof group.items>();
    for (const item of group.items) {
      const key = item.subLabel ?? subgroupOf(item).join(".");
      const list = map.get(key) ?? [];
      list.push(item);
      map.set(key, list);
    }
    return [...map];
  }, [group.items]);
  return (
    <Box>
      <Typography variant="subtitle2" sx={{ color: colors.secondary, mb: 0.5 }}>
        {group.label} · {group.items.length}
      </Typography>
      <Stack spacing={0.5}>
        {subgroups.map(([key, items]) => (
          <SubgroupView
            key={key || "root"}
            subgroupKey={key}
            groupLabel={group.label}
            items={items}
            t={t}
          />
        ))}
      </Stack>
    </Box>
  );
});

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
            <ChangeGroupView key={group.key} group={group} t={t} />
          ))}
        </Stack>
      </DialogContent>
    </B4Dialog>
  );
}

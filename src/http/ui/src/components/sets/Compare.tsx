import { useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  IconButton,
  Stack,
  Switch,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { CompareIcon, SwapIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { B4Select } from "@common/B4Select";
import { B4SetConfig } from "@models/config";
import { formatAsn } from "@models/asn";
import {
  colors,
  facets as facetColors,
  radiusPx,
  spacing,
  typography,
} from "@design";
import { SetStats } from "./Manager";
import {
  FACET_ORDER,
  FacetKey,
  FacetRow,
  SetFacet,
  buildSetFacets,
} from "./facets";

interface SetCompareProps {
  open: boolean;
  sets: B4SetConfig[];
  statsOf: (id: string) => SetStats | undefined;
  initialA: string | null;
  initialB: string | null;
  onClose: () => void;
}

type GroupKey = FacetKey | "tcp" | "udp";

const GROUP_ORDER: GroupKey[] = [...FACET_ORDER, "tcp", "udp"];

const GROUP_COLORS: Record<GroupKey, string> = {
  target: facetColors.target,
  split: facetColors.split,
  fake: facetColors.fake,
  route: facetColors.route,
  dns: facetColors.dns,
  escalate: facetColors.escalate,
  tcp: colors.text.secondary,
  udp: colors.text.secondary,
};

const IGNORE_KEYS = new Set([
  "id",
  "name",
  "enabled",
  "stats",
  "hub_state",
  "hub",
  "manual_domains",
  "manual_ips",
  "geosite_domains",
  "geoip_ips",
  "total_domains",
  "total_ips",
  "asn_ips",
  "geosite_category_breakdown",
  "geoip_category_breakdown",
  "asn_breakdown",
  "asn_unresolved",
]);

const LIST_FIELDS: {
  path: string;
  key: keyof B4SetConfig["targets"];
  label: string;
  format?: (value: string) => string;
}[] = [
  {
    path: "targets.geosite_categories",
    key: "geosite_categories",
    label: "geosite",
  },
  { path: "targets.geoip_categories", key: "geoip_categories", label: "geoip" },
  { path: "targets.asns", key: "asns", label: "asns", format: formatAsn },
  { path: "targets.sni_domains", key: "sni_domains", label: "manualDomains" },
  { path: "targets.ip", key: "ip", label: "manualIps" },
  { path: "targets.source_devices", key: "source_devices", label: "devices" },
];

const LIST_PATHS = new Set(LIST_FIELDS.map((f) => f.path));

const ACTIVITY_PATHS: Record<string, FacetKey> = {
  "routing.enabled": "route",
  "dns.enabled": "dns",
  "faking.sni": "fake",
};
const LIST_PREVIEW = 24;
const DIFF_ONLY_KEY = "b4_sets_compare_diff_only";

const STORAGE = {
  load: (): boolean => {
    try {
      return localStorage.getItem(DIFF_ONLY_KEY) !== "0";
    } catch {
      return true;
    }
  },
  save: (on: boolean) => {
    try {
      localStorage.setItem(DIFF_ONLY_KEY, on ? "1" : "0");
    } catch {
      /* storage unavailable */
    }
  },
};

const groupOfPath = (path: string): GroupKey | null => {
  const [head] = path.split(".");
  if (path === "tcp.dport_filter" || path === "udp.dport_filter")
    return "target";
  switch (head) {
    case "targets":
      return "target";
    case "fragmentation":
    case "mss_clamp":
      return "split";
    case "faking":
      return "fake";
    case "routing":
      return "route";
    case "dns":
      return "dns";
    case "escalate":
      return "escalate";
    case "tcp":
      return "tcp";
    case "udp":
      return "udp";
    default:
      return null;
  }
};

const flattenObject = (
  obj: Record<string, unknown>,
  prefix = "",
): Record<string, unknown> => {
  const result: Record<string, unknown> = {};
  for (const key of Object.keys(obj)) {
    if (IGNORE_KEYS.has(key)) continue;
    const path = prefix ? `${prefix}.${key}` : key;
    const value = obj[key];
    if (value && typeof value === "object" && !Array.isArray(value)) {
      Object.assign(
        result,
        flattenObject(value as Record<string, unknown>, path),
      );
    } else {
      result[path] = value;
    }
  }
  return result;
};

const isZero = (val: unknown): boolean =>
  val === undefined ||
  val === null ||
  val === "" ||
  val === 0 ||
  val === false ||
  (Array.isArray(val) && val.length === 0);

const leafEqual = (a: unknown, b: unknown): boolean => {
  if (isZero(a) && isZero(b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    const sortedA = (a as unknown[]).map((v) => JSON.stringify(v)).sort();
    const sortedB = (b as unknown[]).map((v) => JSON.stringify(v)).sort();
    return sortedA.every((v, i) => v === sortedB[i]);
  }
  return JSON.stringify(a) === JSON.stringify(b);
};

const formatLeaf = (val: unknown, t: (key: string) => string): string => {
  if (isZero(val)) return "—";
  if (Array.isArray(val)) {
    return (val as unknown[]).map((v) => formatLeaf(v, t)).join(", ");
  }
  if (typeof val === "boolean") return t("sets.card.f.on");
  if (typeof val === "string" || typeof val === "number") return String(val);
  return JSON.stringify(val);
};

const leafLabel = (path: string): string =>
  path
    .split(".")
    .slice(1)
    .map((p) => p.replaceAll("_", " "))
    .join(" · ");

interface LeafDiff {
  path: string;
  a: unknown;
  b: unknown;
}

interface ValueCell {
  value: string;
  muted?: string;
}

interface CompareRow {
  label: string;
  a: ValueCell | null;
  b: ValueCell | null;
  differs: boolean;
}

interface ListRow {
  label: string;
  a: string[];
  b: string[];
  differs: boolean;
}

interface CompareGroup {
  key: GroupKey;
  label: string;
  color: string;
  activeA: boolean;
  activeB: boolean;
  rows: CompareRow[];
  allRows: CompareRow[];
  lists: ListRow[];
  leaves: LeafDiff[];
  extraLeaves: LeafDiff[];
  differs: boolean;
}

const cellOf = (row: FacetRow | undefined): ValueCell | null =>
  row ? { value: row.value, muted: row.muted } : null;

const cellsEqual = (a: ValueCell | null, b: ValueCell | null): boolean =>
  a?.value === b?.value && (a?.muted ?? "") === (b?.muted ?? "");

const mergeFacetRows = (
  facetA: SetFacet,
  facetB: SetFacet,
  skip: Set<string>,
  activeOnly: boolean,
): CompareRow[] => {
  const rowsA = facetA.active || !activeOnly ? facetA.rows : [];
  const rowsB = facetB.active || !activeOnly ? facetB.rows : [];
  const labels: string[] = [];
  for (const row of [...rowsA, ...rowsB]) {
    if (!labels.includes(row.label) && !skip.has(row.label)) {
      labels.push(row.label);
    }
  }
  const dormant = !facetA.active && !facetB.active;
  return labels.map((label) => {
    const a = cellOf(rowsA.find((r) => r.label === label));
    const b = cellOf(rowsB.find((r) => r.label === label));
    return { label, a, b, differs: !dormant && !cellsEqual(a, b) };
  });
};

const buildGroups = (
  setA: B4SetConfig,
  setB: B4SetConfig,
  statsA: SetStats | undefined,
  statsB: SetStats | undefined,
  escalateNameA: string | undefined,
  escalateNameB: string | undefined,
  t: (key: string) => string,
): CompareGroup[] => {
  const facetsA = buildSetFacets(setA, statsA, t, escalateNameA);
  const facetsB = buildSetFacets(setB, statsB, t, escalateNameB);

  const flatA = flattenObject(setA as unknown as Record<string, unknown>);
  const flatB = flattenObject(setB as unknown as Record<string, unknown>);
  const leavesByGroup = new Map<GroupKey, LeafDiff[]>();
  const activeOf = (list: SetFacet[], key: FacetKey) =>
    list.find((f) => f.key === key)?.active ?? false;
  for (const path of new Set([...Object.keys(flatA), ...Object.keys(flatB)])) {
    if (LIST_PATHS.has(path)) continue;
    const activity = ACTIVITY_PATHS[path];
    if (
      activity &&
      activeOf(facetsA, activity) !== activeOf(facetsB, activity)
    ) {
      continue;
    }
    const a = flatA[path];
    const b = flatB[path];
    if (leafEqual(a, b)) continue;
    const group = groupOfPath(path);
    if (!group) continue;
    const list = leavesByGroup.get(group) ?? [];
    list.push({ path, a, b });
    leavesByGroup.set(group, list);
  }

  const skipTargetRows = new Set([
    t("sets.card.f.geosite"),
    t("sets.card.f.geoip"),
    t("sets.card.f.asns"),
  ]);

  const lists: ListRow[] = LIST_FIELDS.map((field) => {
    const format = field.format ?? ((value: string) => value);
    const a = ((setA.targets[field.key] as string[] | undefined) ?? []).map(format);
    const b = ((setB.targets[field.key] as string[] | undefined) ?? []).map(format);
    return {
      label: t(`sets.compare.lists.${field.label}`),
      a,
      b,
      differs: !leafEqual(a, b),
    };
  }).filter((row) => row.a.length > 0 || row.b.length > 0);

  return GROUP_ORDER.map((key) => {
    const leaves = (leavesByGroup.get(key) ?? []).sort((x, y) =>
      x.path.localeCompare(y.path),
    );
    const facetA = facetsA.find((f) => f.key === key);
    const facetB = facetsB.find((f) => f.key === key);
    if (!facetA || !facetB) {
      return {
        key,
        label: key.toUpperCase(),
        color: GROUP_COLORS[key],
        activeA: true,
        activeB: true,
        rows: [],
        allRows: [],
        lists: [],
        leaves,
        extraLeaves: leaves,
        differs: leaves.length > 0,
      };
    }
    const skip = key === "target" ? skipTargetRows : new Set<string>();
    const rows = mergeFacetRows(facetA, facetB, skip, true);
    const allRows = mergeFacetRows(facetA, facetB, skip, false);
    const groupLists = key === "target" ? lists : [];
    const shownValues = new Set([
      "—",
      ...allRows
        .flatMap((r) => [r.a, r.b])
        .flatMap((c) =>
          c ? [c.value, c.muted ? `${c.value} ${c.muted}` : c.value] : [],
        ),
    ]);
    const extraLeaves = leaves.filter(
      (leaf) =>
        !shownValues.has(formatLeaf(leaf.a, t)) ||
        !shownValues.has(formatLeaf(leaf.b, t)),
    );
    return {
      key,
      label:
        facetA.label === facetB.label
          ? facetA.label
          : `${facetA.label} / ${facetB.label}`,
      color: GROUP_COLORS[key],
      activeA: facetA.active,
      activeB: facetB.active,
      rows,
      allRows,
      lists: groupLists,
      leaves,
      extraLeaves,
      differs:
        rows.some((r) => r.differs) ||
        groupLists.some((l) => l.differs) ||
        leaves.length > 0,
    };
  });
};

const labelSx = {
  ...typography.recipes.metricLabel,
  fontWeight: typography.weights.bold,
  color: colors.text.disabled,
  pt: "3px",
};

const valueSx = {
  ...typography.recipes.monoSmall,
  fontSize: typography.sizes.sm,
  wordBreak: "break-word" as const,
};

const GRID_COLUMNS = { xs: "84px 1fr 1fr", sm: "120px 1fr 1fr" };

const Cell = ({
  cell,
  differs,
  color,
  dormant,
}: {
  cell: ValueCell | null;
  differs: boolean;
  color: string;
  dormant?: boolean;
}) => (
  <Typography
    component="div"
    sx={{
      ...valueSx,
      color:
        !cell || dormant
          ? colors.text.disabled
          : differs
            ? colors.text.primary
            : colors.text.secondary,
      opacity: (differs || !cell) && !dormant ? 1 : 0.7,
    }}
  >
    {cell ? cell.value : "—"}
    {cell?.muted && (
      <Box
        component="span"
        sx={{ color: differs ? color : colors.text.disabled, ml: spacing.xs }}
      >
        {cell.muted}
      </Box>
    )}
  </Typography>
);

const RowShell = ({
  label,
  differs,
  color,
  children,
}: {
  label: string;
  differs: boolean;
  color: string;
  children: React.ReactNode;
}) => (
  <Box
    sx={{
      display: "grid",
      gridTemplateColumns: GRID_COLUMNS,
      gap: spacing.sm,
      alignItems: "start",
      px: spacing.md,
      py: "6px",
      borderLeft: `2px solid ${differs ? color : "transparent"}`,
      bgcolor: differs ? `${color}0d` : "transparent",
    }}
  >
    <Typography sx={labelSx}>{label}</Typography>
    {children}
  </Box>
);

const ListCell = ({
  items,
  other,
  color,
}: {
  items: string[];
  other: Set<string>;
  color: string;
}) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const ordered = useMemo(
    () =>
      [...items].sort((x, y) => Number(other.has(x)) - Number(other.has(y))),
    [items, other],
  );
  if (items.length === 0) {
    return (
      <Typography sx={{ ...valueSx, color: colors.text.disabled }}>
        —
      </Typography>
    );
  }
  const shown = expanded ? ordered : ordered.slice(0, LIST_PREVIEW);
  const hidden = ordered.length - shown.length;
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: "4px" }}>
      {shown.map((item) => {
        const unique = !other.has(item);
        return (
          <Box
            key={item}
            component="span"
            sx={{
              ...valueSx,
              px: "6px",
              py: "1px",
              borderRadius: `${radiusPx.sm}px`,
              border: `1px solid ${unique ? color : colors.border.light}`,
              color: unique ? colors.text.primary : colors.text.disabled,
              bgcolor: unique ? `${color}1a` : "transparent",
            }}
          >
            {item}
          </Box>
        );
      })}
      {hidden > 0 && (
        <Button
          size="small"
          onClick={() => setExpanded(true)}
          sx={{
            ...valueSx,
            minWidth: 0,
            px: "6px",
            py: 0,
            textTransform: "none",
            color: colors.text.secondary,
          }}
        >
          {t("sets.compare.more", { count: hidden })}
        </Button>
      )}
    </Box>
  );
};

const GroupBlock = ({
  group,
  diffOnly,
  nameA,
  nameB,
}: {
  group: CompareGroup;
  diffOnly: boolean;
  nameA: string;
  nameB: string;
}) => {
  const { t } = useTranslation();

  const rows = diffOnly ? group.rows.filter((r) => r.differs) : group.allRows;
  const lists = diffOnly ? group.lists.filter((l) => l.differs) : group.lists;
  const leaves = group.extraLeaves;

  let status: string | null = null;
  if (!group.activeA && !group.activeB && !group.differs) {
    status = t("sets.compare.noneConfigured");
  } else if (!group.differs) {
    status = t("sets.compare.same");
  } else if (group.activeA && !group.activeB) {
    status = t("sets.compare.onlyIn", { name: nameA });
  } else if (!group.activeA && group.activeB) {
    status = t("sets.compare.onlyIn", { name: nameB });
  }

  const empty = rows.length === 0 && lists.length === 0 && leaves.length === 0;

  return (
    <Box
      sx={{
        border: `1px solid ${colors.border.light}`,
        borderRadius: `${radiusPx.md}px`,
        overflow: "hidden",
        opacity: group.differs ? 1 : 0.75,
      }}
    >
      <Stack
        direction="row"
        alignItems="center"
        gap={spacing.sm}
        sx={{
          px: spacing.md,
          py: "8px",
          bgcolor: colors.background.dark,
          borderBottom: empty ? "none" : `1px solid ${colors.border.light}`,
        }}
      >
        <Box
          sx={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            bgcolor: group.color,
            flexShrink: 0,
          }}
        />
        <Typography
          sx={{
            ...typography.recipes.metricLabel,
            fontWeight: typography.weights.bold,
            color: colors.text.primary,
          }}
        >
          {group.label}
        </Typography>
        <Box sx={{ flex: 1 }} />
        {status && (
          <Typography sx={{ ...valueSx, color: colors.text.disabled }}>
            {status}
          </Typography>
        )}
      </Stack>

      {rows.map((row) => (
        <RowShell
          key={row.label}
          label={row.label}
          differs={row.differs}
          color={group.color}
        >
          <Cell
            cell={row.a}
            differs={row.differs}
            color={group.color}
            dormant={!group.activeA}
          />
          <Cell
            cell={row.b}
            differs={row.differs}
            color={group.color}
            dormant={!group.activeB}
          />
        </RowShell>
      ))}

      {lists.map((row) => (
        <RowShell
          key={row.label}
          label={row.label}
          differs={row.differs}
          color={group.color}
        >
          <ListCell items={row.a} other={new Set(row.b)} color={group.color} />
          <ListCell items={row.b} other={new Set(row.a)} color={group.color} />
        </RowShell>
      ))}

      {leaves.map((leaf) => (
        <RowShell
          key={leaf.path}
          label={leafLabel(leaf.path)}
          differs
          color={group.color}
        >
          <Cell
            cell={{ value: formatLeaf(leaf.a, t) }}
            differs
            color={group.color}
          />
          <Cell
            cell={{ value: formatLeaf(leaf.b, t) }}
            differs
            color={group.color}
          />
        </RowShell>
      ))}
    </Box>
  );
};

const SetPicker = ({
  label,
  value,
  sets,
  onChange,
}: {
  label: string;
  value: string;
  sets: B4SetConfig[];
  onChange: (id: string) => void;
}) => {
  const { t } = useTranslation();
  const selected = sets.find((s) => s.id === value);
  return (
    <Box sx={{ flex: 1, minWidth: 0 }}>
      <B4Select
        label={label}
        value={value}
        options={sets.map((s) => ({ value: s.id, label: s.name || s.id }))}
        onChange={(e) => onChange(String(e.target.value))}
      />
      {selected && (
        <Typography
          sx={{
            ...typography.recipes.metricLabel,
            mt: "4px",
            color: selected.enabled
              ? colors.state.success
              : colors.text.disabled,
          }}
        >
          {selected.enabled ? t("core.enabled") : t("core.disabled")}
        </Typography>
      )}
    </Box>
  );
};

export const SetCompare = ({
  open,
  sets,
  statsOf,
  initialA,
  initialB,
  onClose,
}: SetCompareProps) => {
  const { t } = useTranslation();
  const [picked, setPicked] = useState<{ a: string; b: string } | null>(null);
  const [diffOnly, setDiffOnly] = useState<boolean>(STORAGE.load);

  useEffect(() => {
    if (open) setPicked(null);
  }, [open, initialA, initialB]);

  const aId = picked?.a ?? initialA ?? sets[0]?.id ?? "";
  const bId = picked?.b ?? initialB ?? sets.find((s) => s.id !== aId)?.id ?? "";
  const setAId = (a: string) => setPicked({ a, b: bId });
  const setBId = (b: string) => setPicked({ a: aId, b });

  const setA = sets.find((s) => s.id === aId) ?? null;
  const setB = sets.find((s) => s.id === bId) ?? null;

  const groups = useMemo(() => {
    if (!setA || !setB) return [];
    const escName = (set: B4SetConfig) => {
      const target = set.escalate?.to
        ? sets.find((s) => s.id === set.escalate?.to)
        : undefined;
      return target ? target.name || target.id : undefined;
    };
    return buildGroups(
      setA,
      setB,
      statsOf(setA.id),
      statsOf(setB.id),
      escName(setA),
      escName(setB),
      t,
    );
  }, [setA, setB, sets, statsOf, t]);

  const fieldCount = useMemo(
    () =>
      groups.reduce(
        (sum, g) =>
          sum + g.leaves.length + g.lists.filter((l) => l.differs).length,
        0,
      ),
    [groups],
  );
  const differingGroups = groups.filter((g) => g.differs);

  const toggleDiffOnly = (on: boolean) => {
    setDiffOnly(on);
    STORAGE.save(on);
  };

  const nameA = setA?.name || setA?.id || "";
  const nameB = setB?.name || setB?.id || "";

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      title={t("sets.compare.title")}
      subtitle={setA && setB ? `${nameA} · ${nameB}` : undefined}
      icon={<CompareIcon />}
      maxWidth="md"
      fullWidth
    >
      <Stack gap={spacing.md} sx={{ mt: spacing.sm }}>
        <Stack direction="row" alignItems="flex-start" gap={spacing.sm}>
          <SetPicker
            label={t("sets.compare.setA")}
            value={aId}
            sets={sets}
            onChange={setAId}
          />
          <Tooltip title={t("sets.compare.swap")}>
            <IconButton
              size="small"
              onClick={() => setPicked({ a: bId, b: aId })}
              sx={{ mt: "4px", color: colors.text.secondary }}
            >
              <SwapIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <SetPicker
            label={t("sets.compare.setB")}
            value={bId}
            sets={sets}
            onChange={setBId}
          />
        </Stack>

        {setA && setB && (
          <Stack
            direction="row"
            alignItems="center"
            flexWrap="wrap"
            useFlexGap
            gap={spacing.sm}
          >
            <Typography sx={{ ...valueSx, color: colors.text.secondary }}>
              {setA.id === setB.id
                ? t("sets.compare.sameSet")
                : fieldCount === 0
                  ? t("sets.compare.identical")
                  : `${t("sets.compare.fieldsDiffer", { count: fieldCount })} · ${differingGroups
                      .map((g) => g.label)
                      .join(", ")}`}
            </Typography>
            <Box sx={{ flex: 1 }} />
            <Stack direction="row" alignItems="center" gap="2px">
              <Switch
                size="small"
                checked={diffOnly}
                onChange={(e) => toggleDiffOnly(e.target.checked)}
              />
              <Typography sx={{ ...valueSx, color: colors.text.secondary }}>
                {t("sets.compare.diffOnly")}
              </Typography>
            </Stack>
          </Stack>
        )}

        {setA && setB && setA.id !== setB.id && (
          <>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: GRID_COLUMNS,
                gap: spacing.sm,
                px: spacing.md,
              }}
            >
              <Box />
              <Typography
                sx={{ ...labelSx, color: colors.text.secondary }}
                noWrap
              >
                {nameA}
              </Typography>
              <Typography
                sx={{ ...labelSx, color: colors.text.secondary }}
                noWrap
              >
                {nameB}
              </Typography>
            </Box>
            <Stack gap={spacing.sm}>
              {groups.map((group) => (
                <GroupBlock
                  key={group.key}
                  group={group}
                  diffOnly={diffOnly}
                  nameA={nameA}
                  nameB={nameB}
                />
              ))}
            </Stack>
          </>
        )}
      </Stack>
    </B4Dialog>
  );
};

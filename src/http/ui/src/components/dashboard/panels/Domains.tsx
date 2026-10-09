import { memo, useCallback, useId, useMemo, useState, type FocusEvent, type ReactNode } from "react";
import { Box, IconButton, Tooltip } from "@mui/material";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import { AddIcon, DomainIcon } from "@b4.icons";
import { formatInteger } from "@common/charts";
import { setsApi } from "@api/sets";
import { B4SetConfig, CREATE_SET_SENTINEL } from "@models/config";
import type { DomainHit } from "@models/metrics";
import { AddSniModal } from "@components/connections/AddSniModal";
import { useDomainActions } from "@hooks/useDomainActions";
import { generateDomainVariants } from "@utils";
import { serverNow, shallowEqual, useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { Tag } from "./Tag";
import { formatSince } from "./format";
import { emptySx, listSx, numeric, rowSx } from "./styles";

const DOMAIN_ROWS = 10;
const EMPTY_HITS: readonly DomainHit[] = [];
const NO_NAMES: readonly string[] = [];
const NO_SETS: B4SetConfig[] = [];
const SETS_QUERY_KEY = ["dashboard", "sets"] as const;

interface PendingAdd {
  entry: string;
  setId: string;
  setName: string;
  at: number;
}

interface HeldOrder {
  keys: string[];
  byKey: Map<string, DomainHit>;
}

const addSx = {
  width: 20,
  height: 20,
  p: 0,
  flexShrink: 0,
  border: `1px dashed ${colors.border.default}`,
  borderRadius: `${radiusPx.lg}px`,
  color: colors.text.secondary,
  "& svg": { fontSize: 14 },
  "&:hover": {
    color: colors.secondary,
    borderColor: colors.border.strong,
    bgcolor: colors.accent.secondaryHover,
  },
  "&:focus-visible": {
    outline: `2px solid ${colors.border.strong}`,
    outlineOffset: "2px",
  },
} as const;

const coveredBy = (domain: string, entry: string): boolean =>
  domain === entry || domain.endsWith(`.${entry}`);

const knownSets = (hit: DomainHit, names: ReadonlyMap<string, string>): string[] =>
  hit.sets.filter((id) => names.has(id));

interface DomainRowProps {
  hit: DomainHit;
  setNames: ReadonlyMap<string, string>;
  pending?: PendingAdd;
  onAdd: (domain: string) => void;
}

const DomainRow = memo(function DomainRow({ hit, setNames, pending, onAdd }: DomainRowProps) {
  const { t, i18n } = useTranslation();
  const sets = knownSets(hit, setNames);
  let membership: ReactNode;
  if (sets.length > 0) {
    membership = sets.map((id) => (
      <Tag key={id} label={setNames.get(id) ?? id} to={`/sets/${encodeURIComponent(id)}`} />
    ));
  } else if (pending) {
    membership = (
      <Tag
        label={pending.setName}
        to={pending.setId ? `/sets/${encodeURIComponent(pending.setId)}` : undefined}
      />
    );
  } else {
    membership = (
      <Tooltip title={t("dashboard.domains.add")}>
        <IconButton
          aria-label={t("dashboard.domains.addTo", { domain: hit.key })}
          onClick={() => onAdd(hit.key)}
          sx={addSx}
        >
          <AddIcon />
        </IconButton>
      </Tooltip>
    );
  }
  return (
    <Box component="li" sx={{ ...rowSx, display: "flex", alignItems: "center", gap: "12px" }}>
      <Box
        sx={{
          flex: "1 1 auto",
          minWidth: 0,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          columnGap: "8px",
          rowGap: "4px",
        }}
      >
        <Box
          component="span"
          title={hit.key}
          sx={{
            minWidth: 0,
            maxWidth: "100%",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            fontFamily: fonts.mono,
            fontSize: 12,
            color: colors.text.primary,
          }}
        >
          {hit.key}
        </Box>
        {membership}
      </Box>
      <Box
        component="span"
        sx={{
          ...numeric,
          flexShrink: 0,
          fontSize: 13,
          fontWeight: 700,
          color: colors.text.primary,
        }}
      >
        {formatInteger(hit.count, i18n.language)}
      </Box>
      <Box
        component="span"
        sx={{
          flexShrink: 0,
          minWidth: 72,
          textAlign: "right",
          fontSize: 12,
          color: colors.text.secondary,
        }}
      >
        <Ago at={hit.last} />
      </Box>
    </Box>
  );
});

function DomainsPanelView() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const listId = useId();
  const queryClient = useQueryClient();
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const hits = useMetricsFrame((f) => f.top_domains?.items) ?? EMPTY_HITS;
  const nameList =
    useMetricsFrame((f) => f.sets.flatMap((s) => [s.id, s.name || s.id]), shallowEqual) ??
    NO_NAMES;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;
  const since = formatSince(statsSince, now, locale);
  const [expanded, setExpanded] = useState(false);
  const [held, setHeld] = useState<HeldOrder | null>(null);
  const [pending, setPending] = useState<readonly PendingAdd[]>([]);
  const { modalState, openModal, closeModal, selectVariant, addDomain } = useDomainActions();

  const setsQuery = useQuery({
    queryKey: SETS_QUERY_KEY,
    queryFn: () => setsApi.getSets(),
    enabled: modalState.open,
  });

  const setNames = useMemo(() => {
    const map = new Map<string, string>();
    for (let i = 0; i + 1 < nameList.length; i += 2) map.set(nameList[i], nameList[i + 1]);
    return map;
  }, [nameList]);

  const rows = useMemo(() => {
    if (!held) return hits;
    const live = new Map(hits.map((hit) => [hit.key, hit]));
    return held.keys
      .map((key) => live.get(key) ?? held.byKey.get(key))
      .filter((hit): hit is DomainHit => hit !== undefined);
  }, [hits, held]);

  const onAdd = useCallback(
    (domain: string) => openModal(domain, generateDomainVariants(domain)),
    [openModal],
  );

  const hold = () =>
    setHeld(
      (prev) =>
        prev ?? { keys: hits.map((hit) => hit.key), byKey: new Map(hits.map((hit) => [hit.key, hit])) },
    );
  const release = () => setHeld(null);
  const releaseOnLeave = (e: FocusEvent<HTMLElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget)) release();
  };

  const pendingFor = (hit: DomainHit): PendingAdd | undefined =>
    knownSets(hit, setNames).length > 0
      ? undefined
      : pending.find((p) => hit.last <= p.at && coveredBy(hit.key, p.entry));

  const submit = (setId: string, setName?: string) => {
    const entry = modalState.selected;
    const created = setId === CREATE_SET_SENTINEL;
    const name = created
      ? (setName ?? "")
      : (setsQuery.data?.find((s) => s.id === setId)?.name ?? setName ?? setId);
    void addDomain(setId, setName).then((added) => {
      if (!added) return;
      setPending((prev) => [
        ...prev.filter((p) => p.entry !== entry),
        { entry, setId: created ? "" : setId, setName: name, at: serverNow() },
      ]);
      void queryClient.invalidateQueries({ queryKey: SETS_QUERY_KEY });
    });
  };

  return (
    <>
      <PanelCard
        title={t("dashboard.domains.title")}
        subtitle={t("dashboard.domains.subtitle", { time: since })}
        icon={<DomainIcon />}
        waiting={!hasFrame}
      >
        {rows.length === 0 ? (
          <Box sx={emptySx}>{t("dashboard.domains.empty", { time: since })}</Box>
        ) : (
          <>
            <Box
              component="ul"
              id={listId}
              sx={listSx}
              onMouseEnter={hold}
              onMouseLeave={release}
              onFocus={hold}
              onBlur={releaseOnLeave}
            >
              {visibleRows(rows, expanded, DOMAIN_ROWS).map((hit) => (
                <DomainRow
                  key={hit.key}
                  hit={hit}
                  setNames={setNames}
                  pending={pendingFor(hit)}
                  onAdd={onAdd}
                />
              ))}
            </Box>
            <ShowMore
              total={rows.length}
              expanded={expanded}
              onToggle={() => setExpanded((value) => !value)}
              limit={DOMAIN_ROWS}
              controls={listId}
            />
          </>
        )}
      </PanelCard>
      <AddSniModal
        open={modalState.open}
        domain={modalState.domain}
        variants={modalState.variants}
        selected={modalState.selected}
        sets={setsQuery.data ?? NO_SETS}
        onClose={closeModal}
        onSelectVariant={selectVariant}
        onAdd={submit}
      />
    </>
  );
}

export const DomainsPanel = memo(DomainsPanelView);

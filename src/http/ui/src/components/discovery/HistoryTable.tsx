import { Fragment, useMemo, useState } from "react";
import {
  Box,
  Button,
  Collapse,
  IconButton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  AddIcon,
  CollapseIcon,
  DeleteIcon,
  ExpandIcon,
  RefreshIcon,
} from "@b4.icons";
import { colors, typography } from "@design";
import { B4Badge } from "@b4.elements";
import { B4SetConfig } from "@models/config";
import { HistoryEntry, StrategyFamily } from "@models/discovery";
import {
  Alternate,
  ApplyTarget,
  describeStrategy,
  formatBytes,
  formatTimeAgo,
  historyAlternates,
  historySet,
  historyUnconfirmed,
  historyVerdict,
  presetLabel,
} from "@utils";
import { AlternatesList } from "./AlternatesList";

interface HistoryTableProps {
  entries: HistoryEntry[];
  sets?: B4SetConfig[];
  busy: boolean;
  onApply: (target: ApplyTarget) => void;
  onRerun: (url: string, setId?: string) => void;
  onRemove: (domain: string) => void;
}

interface Row {
  entry: HistoryEntry;
  verdict: ReturnType<typeof historyVerdict>;
  unconfirmed: boolean;
  set: ReturnType<typeof historySet>;
  preset: string;
  sharedWith: string[];
  domains: string[];
  urls: string[];
  alternates: Alternate[];
}

const setKey = (entry: HistoryEntry): string | null => {
  if (!entry.set) return null;
  const domains = [...(entry.set.targets?.sni_domains ?? [])].sort((a, b) =>
    a < b ? -1 : a > b ? 1 : 0,
  );
  if (domains.length < 2) return null;
  return `${entry.suite_id ?? ""}|${entry.set.name ?? ""}|${domains.join(",")}`;
};

export const HistoryTable = ({
  entries,
  sets,
  busy,
  onApply,
  onRerun,
  onRemove,
}: HistoryTableProps) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const rows = useMemo<Row[]>(() => {
    const sorted = [...entries].sort((a, b) => {
      const byTime =
        new Date(b.end_time).getTime() - new Date(a.end_time).getTime();
      if (byTime !== 0) return byTime;
      return (a.order ?? 0) - (b.order ?? 0);
    });
    const urlOf = new Map(
      sorted.map((e) => [e.domain, e.url || `https://${e.domain}/`]),
    );
    const groups = new Map<string, string[]>();
    for (const entry of sorted) {
      const key = setKey(entry);
      if (!key) continue;
      groups.set(key, [...(groups.get(key) ?? []), entry.domain]);
    }
    return sorted.map((entry) => {
      const key = setKey(entry);
      const members = key ? (groups.get(key) ?? []) : [];
      const set = historySet(entry);
      const verdict = historyVerdict(entry);
      const sharedWith = members.filter((d) => d !== entry.domain);
      const domains = [
        entry.domain,
        ...sharedWith.filter((d) => set?.targets.sni_domains.includes(d)),
      ];
      return {
        entry,
        verdict,
        unconfirmed: historyUnconfirmed(entry),
        set,
        preset: entry.set?.name || entry.best_preset,
        sharedWith,
        domains,
        urls: domains.map((d) => urlOf.get(d) ?? `https://${d}/`),
        alternates:
          verdict === "found" ? historyAlternates(entry, domains) : [],
      };
    });
  }, [entries]);

  const setNames = useMemo(
    () => new Map((sets ?? []).map((s) => [s.id, s.name || s.id])),
    [sets],
  );

  const familyName = (family?: StrategyFamily) =>
    family
      ? t(`discovery.familyNames.${family}`, { defaultValue: family })
      : "";

  const badge = (row: Row) => {
    switch (row.verdict) {
      case "found": {
        const tries = row.entry.confirm_tries ?? 0;
        if (!row.unconfirmed && tries > 0) {
          return (
            <B4Badge
              variant="outlined"
              color="success"
              label={t("discovery.status.confirmed", {
                passed: row.entry.confirmed ?? 0,
                tries,
              })}
            />
          );
        }
        return (
          <B4Badge
            variant="outlined"
            color="warning"
            label={
              row.entry.status === "canceled"
                ? t("discovery.status.stoppedEarly")
                : t("discovery.status.notConfirmed")
            }
          />
        );
      }
      case "works_without_bypass":
        return (
          <B4Badge variant="outlined" label={t("discovery.status.noBypass")} />
        );
      case "address_blocked":
        return (
          <B4Badge
            variant="outlined"
            color="error"
            label={t("discovery.status.addressBlocked")}
          />
        );
      case "gateway_intercepted":
        return (
          <B4Badge
            variant="outlined"
            color="error"
            label={t("discovery.status.gatewayIntercepted")}
          />
        );
      default:
        return (
          <B4Badge
            variant="outlined"
            color="warning"
            label={t("discovery.status.nothingFound")}
          />
        );
    }
  };

  const strategy = (row: Row) => {
    const muted = { color: colors.text.secondary };
    switch (row.verdict) {
      case "found": {
        const family =
          row.entry.results?.[row.preset]?.family ?? row.entry.best_family;
        const sentence = row.set ? describeStrategy(row.set, t) : "";
        return (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            <Tooltip title={sentence} placement="top-start">
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: 1,
                  flexWrap: "wrap",
                }}
              >
                <B4Badge
                  variant="outlined"
                  color="primary"
                  label={presetLabel(row.preset, t)}
                  sx={{
                    fontFamily: typography.recipes.monoSmall.fontFamily,
                    fontSize: typography.sizes.sm,
                  }}
                />
                {family && (
                  <Typography variant="body2" sx={muted}>
                    {familyName(family)}
                  </Typography>
                )}
                {row.entry.applied?.[row.preset] && (
                  <B4Badge
                    variant="outlined"
                    color="secondary"
                    label={t("discovery.results.tried")}
                  />
                )}
              </Box>
            </Tooltip>
            {row.sharedWith.length > 0 && (
              <Typography variant="caption" sx={{ color: colors.text.disabled }}>
                {t("discovery.history.sharedWith", {
                  domains: row.sharedWith.join(", "),
                })}
              </Typography>
            )}
            {row.entry.stopped_early && !row.unconfirmed && (
              <Typography variant="caption" sx={{ color: colors.text.disabled }}>
                {t("discovery.history.stoppedEarly")}
              </Typography>
            )}
            {row.alternates.length > 0 && (
              <Button
                size="small"
                endIcon={
                  open[row.entry.domain] ? <CollapseIcon /> : <ExpandIcon />
                }
                onClick={() =>
                  setOpen((prev) => ({
                    ...prev,
                    [row.entry.domain]: !prev[row.entry.domain],
                  }))
                }
                sx={{
                  textTransform: "none",
                  alignSelf: "flex-start",
                  px: 0.5,
                  color: colors.text.secondary,
                }}
              >
                {t("discovery.results.otherStrategies", {
                  count: row.alternates.length,
                })}
              </Button>
            )}
          </Box>
        );
      }
      case "works_without_bypass":
        return (
          <Typography variant="body2" sx={muted}>
            {t("discovery.history.loadsWithout")}
          </Typography>
        );
      case "address_blocked":
        return (
          <Typography variant="body2" sx={muted}>
            {t("discovery.history.needsProxy")}
          </Typography>
        );
      case "gateway_intercepted":
        return (
          <Typography variant="body2" sx={muted}>
            {t("discovery.history.gatewayIntercepted")}
          </Typography>
        );
      default:
        return (
          <Typography variant="body2" sx={muted}>
            {t("discovery.history.nothing")}
          </Typography>
        );
    }
  };

  const applyTarget = (row: Row): ApplyTarget | null =>
    row.set
      ? {
          domains: row.domains,
          set: row.set,
          preset: row.preset,
          urls: row.urls,
          setId: row.entry.set_id,
        }
      : null;

  return (
    <Box sx={{ overflowX: "auto" }}>
      <Table size="small" sx={{ minWidth: 680 }}>
        <TableHead>
          <TableRow>
            <TableCell>{t("discovery.history.site")}</TableCell>
            <TableCell>{t("discovery.history.result")}</TableCell>
            <TableCell sx={{ width: "40%" }}>
              {t("discovery.history.strategy")}
            </TableCell>
            <TableCell sx={{ width: 76 }}>
              {t("discovery.history.when")}
            </TableCell>
            <TableCell align="right" sx={{ width: 190 }} />
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => {
            const target = row.verdict === "found" ? applyTarget(row) : null;
            const expanded = !!open[row.entry.domain];
            const setName = row.entry.set_id
              ? setNames.get(row.entry.set_id)
              : undefined;
            return (
              <Fragment key={row.entry.domain}>
                <TableRow>
                  <TableCell sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>
                    {row.entry.domain}
                    {setName && (
                      <Tooltip
                        title={t("discovery.results.forSet", { name: setName })}
                      >
                        <B4Badge
                          size="small"
                          variant="outlined"
                          label={setName}
                          sx={{
                            display: "flex",
                            width: "fit-content",
                            maxWidth: 180,
                            mt: 0.5,
                            height: 18,
                            fontSize: "0.7rem",
                            fontWeight: 400,
                          }}
                        />
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    {badge(row)}
                  </TableCell>
                  <TableCell sx={{ minWidth: 220 }}>{strategy(row)}</TableCell>
                  <TableCell
                    sx={{ color: colors.text.secondary, whiteSpace: "nowrap" }}
                  >
                    {formatTimeAgo(t, row.entry.end_time, row.entry.start_time)}
                    {!!row.entry.size_bytes && (
                      <Tooltip title={t("discovery.history.sizeHint")}>
                        <Typography
                          variant="caption"
                          sx={{ color: colors.text.disabled, display: "block" }}
                        >
                          {formatBytes(row.entry.size_bytes)}
                        </Typography>
                      </Tooltip>
                    )}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    <Box
                      sx={{
                        display: "inline-flex",
                        alignItems: "center",
                        gap: 0.5,
                      }}
                    >
                      {target && (
                        <Button
                          size="small"
                          variant="contained"
                          startIcon={<AddIcon />}
                          disabled={busy}
                          onClick={() => onApply(target)}
                          sx={{
                            bgcolor: colors.secondary,
                            color: colors.background.default,
                            "&:hover": { bgcolor: colors.primary },
                            whiteSpace: "nowrap",
                          }}
                        >
                          {t("discovery.history.apply")}
                        </Button>
                      )}
                      <Tooltip title={t("discovery.history.runAgain")}>
                        <span>
                          <IconButton
                            size="small"
                            disabled={busy}
                            onClick={() =>
                              onRerun(
                                row.entry.url || row.entry.domain,
                                row.entry.set_id,
                              )
                            }
                            sx={{ color: colors.text.secondary }}
                          >
                            <RefreshIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                      <Tooltip title={t("discovery.history.remove")}>
                        <span>
                          <IconButton
                            size="small"
                            disabled={busy}
                            onClick={() => onRemove(row.entry.domain)}
                            sx={{ color: colors.text.secondary }}
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                    </Box>
                  </TableCell>
                </TableRow>
                {row.alternates.length > 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      sx={{ py: 0, border: expanded ? undefined : 0 }}
                    >
                      <Collapse in={expanded} timeout="auto" unmountOnExit>
                        <Box
                          sx={{
                            py: 1.5,
                            maxWidth: 560,
                            position: "sticky",
                            left: 0,
                          }}
                        >
                          <AlternatesList
                            alternates={row.alternates}
                            applied={row.entry.applied}
                            busy={busy}
                            onUse={(alt) =>
                              onApply({
                                domains: row.domains,
                                set: alt.set,
                                preset: alt.preset,
                                urls: row.urls,
                                setId: row.entry.set_id,
                              })
                            }
                          />
                        </Box>
                      </Collapse>
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            );
          })}
        </TableBody>
      </Table>
    </Box>
  );
};

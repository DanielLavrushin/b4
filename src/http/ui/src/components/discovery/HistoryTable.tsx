import { useMemo } from "react";
import {
  Box,
  Button,
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
import { AddIcon, DeleteIcon, RefreshIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { B4Badge } from "@b4.elements";
import { HistoryEntry, StrategyFamily } from "@models/discovery";
import {
  ApplyTarget,
  describeStrategy,
  formatTimeAgo,
  historySet,
  historyUnconfirmed,
  historyVerdict,
} from "@utils";

interface HistoryTableProps {
  entries: HistoryEntry[];
  busy: boolean;
  onApply: (target: ApplyTarget) => void;
  onRerun: (url: string) => void;
  onRemove: (domain: string) => void;
}

interface Row {
  entry: HistoryEntry;
  verdict: ReturnType<typeof historyVerdict>;
  unconfirmed: boolean;
  set: ReturnType<typeof historySet>;
  preset: string;
  sharedWith: string[];
}

const setKey = (entry: HistoryEntry): string | null => {
  if (!entry.set) return null;
  const domains = [...(entry.set.targets?.sni_domains ?? [])].sort();
  if (domains.length < 2) return null;
  return `${entry.suite_id ?? ""}|${entry.set.name ?? ""}|${domains.join(",")}`;
};

export const HistoryTable = ({
  entries,
  busy,
  onApply,
  onRerun,
  onRemove,
}: HistoryTableProps) => {
  const { t } = useTranslation();

  const rows = useMemo<Row[]>(() => {
    const sorted = [...entries].sort(
      (a, b) => new Date(b.end_time).getTime() - new Date(a.end_time).getTime(),
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
      return {
        entry,
        verdict: historyVerdict(entry),
        unconfirmed: historyUnconfirmed(entry),
        set: historySet(entry),
        preset: entry.set?.name || entry.best_preset,
        sharedWith: members.filter((d) => d !== entry.domain),
      };
    });
  }, [entries]);

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
                  label={row.preset}
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
              </Box>
            </Tooltip>
            {row.sharedWith.length > 0 && (
              <Typography variant="caption" sx={{ color: colors.text.disabled }}>
                {t("discovery.history.sharedWith", {
                  domains: row.sharedWith.join(", "),
                })}
              </Typography>
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
      default:
        return (
          <Typography variant="body2" sx={muted}>
            {t("discovery.history.nothing")}
          </Typography>
        );
    }
  };

  const applyTarget = (row: Row): ApplyTarget | null => {
    if (!row.set) return null;
    const others = row.sharedWith.filter((d) =>
      row.set!.targets.sni_domains.includes(d),
    );
    return {
      domains: [row.entry.domain, ...others],
      set: row.set,
      preset: row.preset,
    };
  };

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
            return (
              <TableRow
                key={row.entry.domain}
                sx={{ "&:last-child td": { border: 0 } }}
              >
                <TableCell sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>
                  {row.entry.domain}
                </TableCell>
                <TableCell sx={{ whiteSpace: "nowrap" }}>{badge(row)}</TableCell>
                <TableCell sx={{ minWidth: 220 }}>{strategy(row)}</TableCell>
                <TableCell
                  sx={{ color: colors.text.secondary, whiteSpace: "nowrap" }}
                >
                  {formatTimeAgo(t, row.entry.end_time, row.entry.start_time)}
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
                            onRerun(row.entry.url || row.entry.domain)
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
            );
          })}
        </TableBody>
      </Table>
    </Box>
  );
};

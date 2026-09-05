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
import { DeleteIcon } from "@b4.icons";
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
  sameAs?: string;
}

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
    const seen = new Map<string, string>();
    return sorted.map((entry) => {
      const verdict = historyVerdict(entry);
      const row: Row = {
        entry,
        verdict,
        unconfirmed: historyUnconfirmed(entry),
        set: historySet(entry),
      };
      if (verdict === "found" && entry.suite_id && entry.set) {
        const key = `${entry.suite_id}|${entry.best_preset}`;
        const first = seen.get(key);
        if (first) row.sameAs = first;
        else seen.set(key, entry.domain);
      }
      return row;
    });
  }, [entries]);

  const familyName = (family?: StrategyFamily) =>
    family ? t(`discovery.familyNames.${family}`, { defaultValue: family }) : "";

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
      case "found":
        if (row.sameAs) {
          return (
            <Typography variant="body2" sx={muted}>
              {t("discovery.history.sameAs", { domain: row.sameAs })}
            </Typography>
          );
        }
        return (
          <Box>
            <Typography variant="body2">
              {row.set
                ? describeStrategy(row.set, t)
                : familyName(row.entry.best_family)}
            </Typography>
            <Typography
              sx={{
                ...typography.recipes.monoSmall,
                color: colors.text.disabled,
              }}
            >
              {row.entry.best_preset}
            </Typography>
          </Box>
        );
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
            <TableCell align="right" sx={{ width: 180 }} />
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => (
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
                {row.verdict === "found" && row.set && (
                  <Button
                    size="small"
                    disabled={busy}
                    onClick={() =>
                      onApply({
                        domains: row.set!.targets.sni_domains.length
                          ? row.set!.targets.sni_domains
                          : [row.entry.domain],
                        set: row.set!,
                        preset: row.entry.best_preset,
                      })
                    }
                    sx={{ textTransform: "none", minWidth: 0, px: 1 }}
                  >
                    {t("discovery.history.apply")}
                  </Button>
                )}
                <Button
                  size="small"
                  disabled={busy}
                  onClick={() => onRerun(row.entry.url || row.entry.domain)}
                  sx={{
                    textTransform: "none",
                    color: colors.text.secondary,
                    minWidth: 0,
                    px: 1,
                  }}
                >
                  {t("discovery.history.runAgain")}
                </Button>
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
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
};

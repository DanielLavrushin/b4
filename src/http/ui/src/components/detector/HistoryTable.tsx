import { Box, Button, IconButton, Table, TableBody, TableCell, TableHead, TableRow, Tooltip, Typography } from "@mui/material";
import { CopyIcon, DeleteIcon } from "@b4.icons";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { DetectorSuite } from "@models/detector";

interface HistoryTableProps {
  entries: DetectorSuite[];
  currentId?: string | null;
  onOpen: (entry: DetectorSuite) => void;
  onCopy: (entry: DetectorSuite) => void;
  onDelete: (id: string) => void;
}

export function historySummary(e: DetectorSuite, t: (k: string, o?: Record<string, unknown>) => string): string {
  const v = e.verdict;
  const parts: string[] = [];
  if (e.sites) {
    parts.push(t("detector.history.blocked", { count: v.blocked_by_isp }));
    if (e.options.fetch_mode !== "direct") {
      parts.push(t("detector.history.fixed", { count: v.fixed_by_b4 }));
      if (v.still_blocked > 0) parts.push(t("detector.history.still", { count: v.still_blocked }));
    }
  }
  if (e.dns) {
    if (v.dns_hijacked) parts.push(t("detector.history.dnsHijacked"));
    else if (v.dns_substituted) parts.push(t("detector.history.dnsSubstituted"));
    else if (e.dns.udp_ok > 0) parts.push(t("detector.history.dnsHonest"));
  }
  if (e.hosting) {
    parts.push(v.dropped_networks?.length ? t("detector.history.hostingDropped", { nets: v.dropped_networks.slice(0, 3).join(", ") }) : t("detector.history.hostingClean"));
  }
  if (e.telegram) parts.push(t("detector.history.telegram", { verdict: t(`detector.telegram.${e.telegram.verdict || "error"}`) }));
  if (e.status === "canceled") parts.unshift(t("detector.history.stopped"));
  return parts.join(" · ");
}

export const HistoryTable = ({ entries, currentId, onOpen, onCopy, onDelete }: HistoryTableProps) => {
  const { t } = useTranslation();
  return (
    <Box sx={{ overflowX: "auto" }}>
      <Table size="small" sx={{ minWidth: 640 }}>
        <TableHead>
          <TableRow>
            <TableCell>{t("detector.history.when")}</TableCell>
            <TableCell>{t("detector.history.checked")}</TableCell>
            <TableCell sx={{ width: "45%" }}>{t("detector.history.verdict")}</TableCell>
            <TableCell align="right" />
          </TableRow>
        </TableHead>
        <TableBody>
          {entries.map((e) => (
            <TableRow key={e.id} sx={{ "&:last-child td": { border: 0 }, bgcolor: e.id === currentId ? colors.accent.secondaryHover : undefined }}>
              <TableCell sx={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>{new Date(e.start_time).toLocaleString()}</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap", color: colors.text.secondary }}>
                {[e.sites ? t("detector.history.sites", { count: (e.sites.sites ?? []).length }) : "", ...(e.options.scopes ?? []).filter((s) => s !== "sites").map((s) => t(`detector.scopes.${s}.name`))].filter(Boolean).join(" · ")}
              </TableCell>
              <TableCell sx={{ color: colors.text.secondary, fontSize: "0.8rem" }}>{historySummary(e, t)}</TableCell>
              <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                  {e.id === currentId ? (
                    <Typography variant="caption" sx={{ color: colors.text.disabled, mr: 1 }}>{t("detector.history.thisRun")}</Typography>
                  ) : (
                    <Button
                      size="small"
                      variant="contained"
                      onClick={() => onOpen(e)}
                      sx={{ bgcolor: colors.secondary, color: colors.background.default, "&:hover": { bgcolor: colors.primary }, whiteSpace: "nowrap" }}
                    >
                      {t("detector.history.open")}
                    </Button>
                  )}
                  <Tooltip title={t("detector.verdict.copyReport")}>
                    <IconButton size="small" onClick={() => onCopy(e)} sx={{ color: colors.text.secondary }}>
                      <CopyIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title={t("core.history.removeFromHistory")}>
                    <IconButton size="small" onClick={() => onDelete(e.id)} sx={{ color: colors.text.secondary }}>
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </Box>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
};

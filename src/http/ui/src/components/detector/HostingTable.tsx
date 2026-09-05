import { useState } from "react";
import { Box, Button, Collapse, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { HostingGroup, HostingResult } from "@models/detector";
import { StatusChip, hostingColor } from "./statuses";

interface HostingTableProps {
  result: HostingResult;
  onCopySNI: (sni: string) => void;
}

export function hostingLead(result: HostingResult, dropped: string[], t: (k: string, o?: Record<string, unknown>) => string): string {
  if (result.dropped_groups === 0) return t("detector.hosting.leadClean", { total: result.total });
  return t("detector.hosting.leadDropped", { nets: dropped.slice(0, 5).join(", "), count: result.dropped_groups, all: result.groups.length });
}

const dropRange = (g: HostingGroup) => {
  if (!g.dropped) return "-";
  return g.drop_min_kb === g.drop_max_kb ? `${g.drop_min_kb} KB` : `${g.drop_min_kb}-${g.drop_max_kb} KB`;
};

export const HostingTable = ({ result, onCopySNI }: HostingTableProps) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState<string | null>(null);
  const droppedNames = result.groups.filter((g) => g.status === "dropped" || g.status === "mixed").map((g) => g.provider);

  return (
    <Stack spacing={1.5}>
      <Typography variant="body2" sx={{ color: colors.text.secondary, maxWidth: "90ch" }}>
        {hostingLead(result, droppedNames, t)}
      </Typography>
      <Box sx={{ overflowX: "auto" }}>
        <Table size="small" sx={{ minWidth: 720 }}>
          <TableHead>
            <TableRow>
              <TableCell>{t("detector.hosting.network")}</TableCell>
              <TableCell>{t("detector.hosting.result")}</TableCell>
              <TableCell>{t("detector.hosting.targets")}</TableCell>
              <TableCell>{t("detector.hosting.dropsAt")}</TableCell>
              <TableCell>{t("detector.hosting.workingSni")}</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {result.groups.map((g) => {
              const key = `${g.asn}-${g.provider}`;
              const expanded = open === key;
              const done = g.targets.filter((x) => x.done).length;
              return [
                <TableRow key={key} sx={{ "& td": { borderBottom: expanded ? 0 : undefined } }}>
                  <TableCell sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>
                    {g.provider}
                    <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.75 }}>
                      AS{g.asn}
                    </Typography>
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    {g.status ? (
                      <StatusChip
                        label={t(`detector.hosting.status.${g.status}`, { dropped: g.dropped, total: g.total })}
                        color={hostingColor(g.status)}
                      />
                    ) : (
                      <StatusChip label={t("detector.status.CHECKING")} color="info" />
                    )}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap", color: colors.text.secondary, fontVariantNumeric: "tabular-nums" }}>
                    {t("detector.hosting.counts", { ok: g.ok, dropped: g.dropped, none: g.timeouts, pending: g.total - done })}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>{dropRange(g)}</TableCell>
                  <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8rem" }}>
                    {g.working_snis && g.working_snis.length > 0
                      ? g.working_snis.map((sni) => (
                          <Button key={sni} size="small" onClick={() => onCopySNI(sni)} sx={{ textTransform: "none", fontFamily: "inherit", fontSize: "inherit", py: 0 }} title={t("detector.hosting.copySni")}>
                            {sni}
                          </Button>
                        ))
                      : g.sni_searched
                        ? <Typography variant="caption" sx={{ color: colors.text.disabled }}>{t("detector.hosting.noSni")}</Typography>
                        : ""}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    <Button size="small" onClick={() => setOpen(expanded ? null : key)}>
                      {expanded ? t("detector.hosting.hide") : t("detector.hosting.details")}
                    </Button>
                  </TableCell>
                </TableRow>,
                <TableRow key={`${key}-d`}>
                  <TableCell colSpan={6} sx={{ p: 0, border: expanded ? undefined : 0 }}>
                    <Collapse in={expanded} unmountOnExit>
                      <Box sx={{ px: 2, py: 1, bgcolor: colors.background.control }}>
                        <Table size="small">
                          <TableBody>
                            {g.targets.map((tr) => (
                              <TableRow key={tr.target.id} sx={{ "&:last-child td": { border: 0 } }}>
                                <TableCell sx={{ fontFamily: "monospace", fontSize: "0.78rem", whiteSpace: "nowrap" }}>
                                  {tr.target.ip}:{tr.target.port}
                                </TableCell>
                                <TableCell sx={{ whiteSpace: "nowrap" }}>
                                  {tr.done ? (
                                    <StatusChip label={t(`detector.hosting.target.${tr.status || "error"}`)} color={hostingColor(tr.status)} />
                                  ) : (
                                    <StatusChip label={t("detector.status.PENDING")} color="default" />
                                  )}
                                </TableCell>
                                <TableCell sx={{ color: colors.text.secondary, fontSize: "0.78rem" }}>
                                  {[tr.detail, tr.rtt_ms ? `RTT ${tr.rtt_ms} ms` : "", tr.target.sni ? `SNI ${tr.target.sni}` : ""].filter(Boolean).join(" · ")}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </Box>
                    </Collapse>
                  </TableCell>
                </TableRow>,
              ];
            })}
          </TableBody>
        </Table>
      </Box>
    </Stack>
  );
};

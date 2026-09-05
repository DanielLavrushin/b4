import { Box, Button, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { DNSProbe, DNSProvider, DNSResult } from "@models/detector";
import { StatusChip, dnsProbeColor, honestyColor } from "./statuses";

interface DnsTableProps {
  result: DNSResult;
  onUseDoH: (url: string) => void;
}

const Cell = ({ probe }: { probe?: DNSProbe }) => {
  const { t } = useTranslation();
  if (!probe) return <Typography variant="caption" sx={{ color: colors.text.disabled }}>-</Typography>;
  if (probe.status === "ok") {
    return (
      <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums" }} title={probe.address}>
        {probe.latency_ms} ms
      </Typography>
    );
  }
  return <StatusChip label={t(`detector.dns.probe.${probe.status}`)} color={dnsProbeColor(probe.status)} title={probe.detail || probe.address} />;
};

function honesty(p: DNSProvider): DNSProbe | undefined {
  return [p.udp, p.doh, p.dot].find((x) => x && x.status === "ok" && x.honesty && x.honesty !== "unknown");
}

export function dnsLead(result: DNSResult, t: (k: string, o?: Record<string, unknown>) => string): string {
  const parts: string[] = [];
  if (result.hijacked > 0) {
    parts.push(t("detector.dns.leadHijacked", { by: result.hijacked_by || t("detector.verdict.unknownParty"), count: result.hijacked, total: result.udp_total }));
  } else if (result.udp_ok > 0) {
    parts.push(t("detector.dns.leadNotHijacked"));
  } else if (result.udp_total > 0) {
    parts.push(t("detector.dns.leadUdpDead"));
  }
  if (result.substituting > 0) parts.push(t("detector.dns.leadSubstituting", { count: result.substituting }));
  if (result.stub_ips?.length) parts.push(t("detector.dns.leadStubs", { ips: result.stub_ips.join(", ") }));
  parts.push(
    result.doh_ok + result.dot_ok > 0
      ? t("detector.dns.leadEncrypted", { doh: result.doh_ok, dohTotal: result.doh_total, dot: result.dot_ok, dotTotal: result.dot_total })
      : t("detector.dns.leadEncryptedBlocked"),
  );
  if (!result.truth_available) parts.push(t("detector.dns.leadNoTruth"));
  return parts.join(" ");
}

export const DnsTable = ({ result, onUseDoH }: DnsTableProps) => {
  const { t } = useTranslation();
  const honestDoH = new Set(result.honest_doh ?? []);

  return (
    <Stack spacing={1.5}>
      <Typography variant="body2" sx={{ color: colors.text.secondary, maxWidth: "90ch" }}>
        {dnsLead(result, t)}
      </Typography>
      <Box sx={{ overflowX: "auto" }}>
        <Table size="small" sx={{ minWidth: 760 }}>
          <TableHead>
            <TableRow>
              <TableCell>{t("detector.dns.provider")}</TableCell>
              <TableCell>UDP 53</TableCell>
              <TableCell>DoH</TableCell>
              <TableCell>DoT</TableCell>
              <TableCell>{t("detector.dns.honest")}</TableCell>
              <TableCell>{t("detector.dns.answeredBy")}</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {result.providers.map((p) => {
              const h = honesty(p);
              const udp = p.udp;
              const by = udp?.answered_by
                ? [udp.answered_by_org || (udp.answered_by_asn ? `AS${udp.answered_by_asn}` : udp.answered_by)]
                : [];
              return (
                <TableRow key={p.name} sx={{ "&:last-child td": { border: 0 } }}>
                  <TableCell sx={{ fontWeight: 600, whiteSpace: "nowrap" }}>
                    {p.name}
                    {p.router && (
                      <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                        {t("detector.dns.routerResolver")}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}><Cell probe={p.udp} /></TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}><Cell probe={p.doh} /></TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}><Cell probe={p.dot} /></TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    {h ? (
                      <StatusChip
                        label={t(`detector.dns.honesty.${h.honesty}`)}
                        color={honestyColor(h.honesty)}
                        title={h.substituted ? t("detector.dns.substitutedOf", { n: h.substituted, total: h.checked }) : undefined}
                      />
                    ) : (
                      <Typography variant="caption" sx={{ color: colors.text.disabled }}>-</Typography>
                    )}
                  </TableCell>
                  <TableCell sx={{ color: udp?.hijacked ? colors.state.error : colors.text.secondary, fontSize: "0.8rem" }} title={udp?.answered_by}>
                    {by.length > 0 ? by.join(" ") : udp?.status === "ok" ? t("detector.dns.noEgress") : ""}
                    {udp?.hijacked && ` · ${t("detector.dns.hijackedMark")}`}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    {p.doh && p.doh.status === "ok" && honestDoH.has(p.doh.address) && (
                      <Button size="small" onClick={() => onUseDoH(p.doh!.address)} sx={{ textTransform: "none" }}>
                        {t("detector.dns.useDoH")}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </Box>
    </Stack>
  );
};

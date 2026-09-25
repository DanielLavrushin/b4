import { Box, Button, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { DNSHonesty, DNSProbe, DNSProvider, DNSResult } from "@models/detector";
import { StatusChip, dnsProbeColor, honestyColor } from "./statuses";

type Translate = (k: string, o?: Record<string, unknown>) => string;

interface DnsTableProps {
  result: DNSResult;
  onUseDoH: (url: string) => void;
}

export interface HonestyGroup {
  honesty: DNSHonesty;
  transports: string[];
  probes: DNSProbe[];
}

const HONESTY_ORDER: DNSHonesty[] = ["substituted", "no_answer", "filtered", "differs", "honest"];
const TRANSPORTS = [
  ["UDP", "udp"],
  ["DoH", "doh"],
  ["DoT", "dot"],
] as const;

export function honestyGroups(p: DNSProvider): HonestyGroup[] {
  const judged = TRANSPORTS.flatMap(([name, key]) => {
    const probe = p[key];
    return probe?.status === "ok" && probe.honesty && probe.honesty !== "unknown" ? [{ name, probe, honesty: probe.honesty }] : [];
  });
  return HONESTY_ORDER.map((honesty) => {
    const hits = judged.filter((j) => j.honesty === honesty);
    return { honesty, transports: hits.map((j) => j.name), probes: hits.map((j) => j.probe) };
  }).filter((g) => g.probes.length > 0);
}

export function providersJudged(result: DNSResult, honesty: "substituted" | "no_answer"): string[] {
  const saved = honesty === "substituted" ? result.substituting_by : result.no_answer_by;
  if (saved) return saved;
  return (result.providers ?? []).filter((p) => honestyGroups(p).some((g) => g.honesty === honesty)).map((p) => p.name);
}

export function sameEgress(result: DNSResult): string | undefined {
  const rows = (result.providers ?? []).flatMap((p) => (!p.router && p.udp?.status === "ok" && p.udp.answered_by_asn ? [p.udp] : []));
  if (rows.length < 5) return undefined;
  const asn = rows[0].answered_by_asn;
  if (rows.some((r) => r.answered_by_asn !== asn)) return undefined;
  return rows.find((r) => r.answered_by_org)?.answered_by_org || `AS${asn}`;
}

function verdictCounts(g: HonestyGroup, t: Translate): string | undefined {
  const probe = g.probes.find((x) => (x.checked ?? 0) > 0);
  if (!probe) return undefined;
  return t("detector.dns.verdictCounts", {
    checked: probe.checked ?? 0,
    substituted: probe.substituted ?? 0,
    noAnswer: probe.no_answer ?? 0,
    filtered: probe.filtered ?? 0,
  });
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

const HonestyCell = ({ provider }: { provider: DNSProvider }) => {
  const { t } = useTranslation();
  const groups = honestyGroups(provider);
  if (groups.length === 0) return <Typography variant="caption" sx={{ color: colors.text.disabled }}>-</Typography>;
  return (
    <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
      {groups.map((g) => {
        const verdict = t(`detector.dns.honesty.${g.honesty}`);
        return (
          <StatusChip
            key={g.honesty}
            label={groups.length > 1 ? `${verdict} · ${g.transports.join("/")}` : verdict}
            color={honestyColor(g.honesty)}
            title={verdictCounts(g, t)}
          />
        );
      })}
    </Stack>
  );
};

export function dnsLead(result: DNSResult, t: Translate): string {
  const parts: string[] = [];
  const egress = sameEgress(result);
  if (result.hijacked > 0) {
    parts.push(t("detector.dns.leadHijacked", { by: result.hijacked_by || t("detector.verdict.unknownParty"), count: result.hijacked, total: result.udp_total }));
  } else if (egress) {
    parts.push(t("detector.dns.leadSameEgress", { by: egress }));
  } else if (result.udp_ok > 0) {
    parts.push(t("detector.dns.leadNotHijacked"));
  } else if (result.udp_total > 0) {
    parts.push(t("detector.dns.leadUdpDead"));
  }
  const substituting = providersJudged(result, "substituted");
  const noAnswer = providersJudged(result, "no_answer");
  if (substituting.length) parts.push(t("detector.dns.leadSubstituting", { names: substituting.join(", ") }));
  if (noAnswer.length) parts.push(t("detector.dns.leadNoAnswer", { names: noAnswer.join(", ") }));
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
            {(result.providers ?? []).map((p) => {
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
                    <HonestyCell provider={p} />
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

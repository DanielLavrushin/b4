import { Box, Button, Paper, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { DiscoveryIcon, CopyIcon, RefreshIcon } from "@b4.icons";
import { colors } from "@design";
import type { DetectorSuite } from "@models/detector";

interface VerdictPanelProps {
  suite: DetectorSuite;
  running: boolean;
  onDiscovery: (sites: string[]) => void;
  onCopy: () => void;
  onRunAgain: () => void;
}

function pickKinds(kinds?: Record<string, number>): string[] {
  if (!kinds) return [];
  return Object.entries(kinds)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 2)
    .map(([k]) => k);
}

export function verdictSentences(suite: DetectorSuite, t: (k: string, o?: Record<string, unknown>) => string): { title: string; body: string[] } {
  const v = suite.verdict;
  const body: string[] = [];
  let title = "";
  const partial = suite.status === "canceled";

  if (suite.sites) {
    const kinds = pickKinds(v.block_kinds).map((k) => t(`detector.kind.${k}`, { defaultValue: k.toLowerCase() }));
    if (v.blocked_by_isp === 0 && v.sites > 0 && suite.sites.ok === v.sites) {
      title = t("detector.verdict.titleClean");
    } else if (v.blocked_by_isp > 0 && kinds.length > 0) {
      title = t("detector.verdict.titleBlocks", { kinds: kinds.join(t("detector.verdict.and")) });
    } else if (v.blocked_by_isp > 0) {
      title = t("detector.verdict.titleBlocked");
    }
    body.push(
      t("detector.verdict.sites", {
        blocked: v.blocked_by_isp,
        total: v.sites,
      }),
    );
    if (suite.options.fetch_mode !== "direct" && v.blocked_by_isp > 0) {
      body.push(t("detector.verdict.fixed", { fixed: v.fixed_by_b4 }));
      if (v.still_blocked > 0) {
        body.push(
          t("detector.verdict.stillBlocked", {
            count: v.still_blocked,
            sites: (v.still_blocked_sites ?? []).slice(0, 4).map((u) => u.replace(/^https?:\/\//, "").split("/")[0]).join(", "),
          }),
        );
      }
    }
    if (v.broken_by_b4 > 0) {
      body.push(t("detector.verdict.brokenByB4", { count: v.broken_by_b4 }));
    }
  }
  if (suite.dns) {
    const d = suite.dns;
    if (d.hijacked > 0) {
      body.push(t("detector.verdict.dnsHijacked", { by: d.hijacked_by || t("detector.verdict.unknownParty") }));
    } else if (d.substituting > 0) {
      body.push(t("detector.verdict.dnsSubstituted", { count: d.substituting }));
    } else if (d.udp_ok > 0) {
      body.push(t("detector.verdict.dnsHonest"));
    }
    if (d.doh_total > 0) {
      body.push(
        d.doh_ok > 0
          ? t("detector.verdict.dohWorks", { doh: d.doh_ok, dohTotal: d.doh_total, dot: d.dot_ok, dotTotal: d.dot_total })
          : t("detector.verdict.dohBlocked"),
      );
    }
    if (!title) title = d.hijacked > 0 ? t("detector.verdict.titleDnsHijacked") : t("detector.verdict.titleDns");
  }
  if (suite.hosting) {
    const h = suite.hosting;
    body.push(
      h.dropped_groups > 0
        ? t("detector.verdict.hostingDropped", { nets: (v.dropped_networks ?? []).slice(0, 4).join(", "), count: h.dropped_groups })
        : t("detector.verdict.hostingClean"),
    );
    if (!title) title = h.dropped_groups > 0 ? t("detector.verdict.titleHosting") : t("detector.verdict.titleHostingClean");
  }
  if (suite.telegram) {
    body.push(t("detector.verdict.telegram", { verdict: t(`detector.telegram.${suite.telegram.verdict || "error"}`) }));
    if (!title) title = t("detector.verdict.titleTelegram");
  }
  if (!title) title = t("detector.verdict.titleEmpty");
  if (partial) body.unshift(t("detector.verdict.partial"));
  return { title, body };
}

export const VerdictPanel = ({ suite, running, onDiscovery, onCopy, onRunAgain }: VerdictPanelProps) => {
  const { t } = useTranslation();
  const v = suite.verdict;
  const { title, body } = verdictSentences(suite, t);
  const stillBlocked = v.still_blocked_sites ?? [];
  const both = suite.options.fetch_mode !== "direct";

  const started = new Date(suite.start_time);
  const ended = suite.end_time ? new Date(suite.end_time) : null;
  const durationSec = ended && ended.getFullYear() > 1970 ? Math.max(0, Math.round((ended.getTime() - started.getTime()) / 1000)) : 0;
  const meta = [
    started.toLocaleString(),
    (suite.options.scopes ?? []).map((s) => t(`detector.scopes.${s}.name`)).join(" · "),
    durationSec > 0 && t("detector.verdict.duration", { seconds: durationSec }),
    suite.network?.wan_ip && [suite.network.wan_ip, suite.network.asn && `AS${suite.network.asn}`, suite.network.org].filter(Boolean).join(" "),
  ]
    .filter(Boolean)
    .join(" · ");

  const counters: { value: number; label: string; color: string }[] = [];
  if (suite.sites) {
    counters.push({ value: v.blocked_by_isp, label: t("detector.verdict.countBlocked"), color: colors.state.error });
    if (both) {
      counters.push({ value: v.fixed_by_b4, label: t("detector.verdict.countFixed"), color: colors.state.success });
      counters.push({ value: v.still_blocked, label: t("detector.verdict.countStill"), color: colors.state.warning });
    }
    counters.push({ value: v.not_blocked, label: t("detector.verdict.countOk"), color: colors.text.primary });
  }

  return (
    <Paper
      variant="outlined"
      sx={{
        p: 3,
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.strong}`,
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "1fr auto" },
        gap: 3,
        alignItems: "start",
      }}
    >
      <Box sx={{ minWidth: 0 }}>
        <Typography sx={{ fontSize: 19, fontWeight: 600, textWrap: "balance", mb: 0.5 }}>
          {running ? t("detector.verdict.titleRunning") : title}
        </Typography>
        <Typography variant="body2" sx={{ color: colors.text.secondary, maxWidth: "70ch" }}>
          {body.join(" ")}
        </Typography>
        {counters.length > 0 && (
          <Stack direction="row" spacing={3} sx={{ mt: 1.5 }} flexWrap="wrap" useFlexGap>
            {counters.map((c) => (
              <Box key={c.label}>
                <Typography sx={{ fontSize: 22, fontWeight: 600, lineHeight: 1.1, color: c.color, fontVariantNumeric: "tabular-nums" }}>
                  {c.value}
                </Typography>
                <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                  {c.label}
                </Typography>
              </Box>
            ))}
          </Stack>
        )}
      </Box>
      <Stack spacing={1} sx={{ minWidth: 220 }}>
        {stillBlocked.length > 0 && !running && (
          <Button variant="contained" startIcon={<DiscoveryIcon />} onClick={() => onDiscovery(stillBlocked)}>
            {t("detector.verdict.fixWithDiscovery", { count: stillBlocked.length })}
          </Button>
        )}
        <Button variant="outlined" startIcon={<CopyIcon />} onClick={onCopy}>
          {t("detector.verdict.copyReport")}
        </Button>
        {!running && (
          <Button variant="outlined" startIcon={<RefreshIcon />} onClick={onRunAgain}>
            {t("detector.verdict.runAgain")}
          </Button>
        )}
        <Typography variant="caption" sx={{ color: colors.text.disabled }}>
          {meta}
        </Typography>
      </Stack>
    </Paper>
  );
};

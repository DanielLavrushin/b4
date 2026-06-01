import { Box, Grid, Stack, Typography } from "@mui/material";
import { motion } from "motion/react";
import { DnsIcon, DomainIcon, NetworkIcon, SniIcon, SpeedIcon, ConnectionIcon } from "@b4.icons";
import { colors, spacing, radius } from "@design";
import { B4Card } from "@common/B4Card";
import type { DetectorSuite } from "@models/detector";
import { staggerContainer, staggerItem, statusColors } from "./constants";
import { useTranslation } from "react-i18next";

interface SummaryCardProps {
  title: string;
  value: string;
  subtitle: string;
  icon: React.ReactNode;
  color: string;
}

function SummaryCard({ title, value, subtitle, icon, color }: SummaryCardProps) {
  return (
    <B4Card
      variant="outlined"
      sx={{
        border: `1px solid ${color}33`,
        transition: "all 0.2s ease",
        "&:hover": {
          borderColor: `${color}66`,
          boxShadow: `0 0 20px ${color}22`,
        },
      }}
    >
      <Box sx={{ p: spacing.md }}>
        <Stack
          direction="row"
          justifyContent="space-between"
          alignItems="flex-start"
        >
          <Box sx={{ flex: 1 }}>
            <Typography
              variant="caption"
              sx={{
                color: colors.text.secondary,
                textTransform: "uppercase",
                letterSpacing: "0.5px",
              }}
            >
              {title}
            </Typography>
            <Typography
              variant="h4"
              sx={{
                color: colors.text.primary,
                fontWeight: 600,
                mt: 0.5,
                mb: 0.5,
              }}
            >
              {value}
            </Typography>
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {subtitle}
            </Typography>
          </Box>
          <Box
            sx={{
              p: 1.5,
              borderRadius: radius.lg,
              bgcolor: `${color}22`,
              color,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              minWidth: 56,
              minHeight: 56,
            }}
          >
            {icon}
          </Box>
        </Stack>
      </Box>
    </B4Card>
  );
}

export function SummaryDashboard({
  suite,
}: Readonly<{ suite: DetectorSuite }>) {
  const { t } = useTranslation();
  const cards: SummaryCardProps[] = [];

  if (suite.dns_result) {
    const r = suite.dns_result;
    const bad = r.spoof_count + r.intercept_count;
    cards.push({
      title: t("detector.summary.dnsIntegrity"),
      value: bad > 0 ? t(bad > 1 ? "detector.summary.issues_plural" : "detector.summary.issues", { count: bad }) : t("detector.summary.allOk"),
      subtitle: r.summary,
      icon: <DnsIcon sx={{ fontSize: 28 }} />,
      color: bad > 0 ? statusColors.error : statusColors.ok,
    });
  }

  if (suite.dnsavail_result) {
    const r = suite.dnsavail_result;
    const okAny = r.doh_ok > 0 || r.udp_ok > 0;
    cards.push({
      title: t("detector.summary.dnsAvailability"),
      value: `${r.doh_ok + r.udp_ok}/${r.doh_total + r.udp_total}`,
      subtitle: r.summary,
      icon: <SpeedIcon sx={{ fontSize: 28 }} />,
      color: okAny ? statusColors.ok : statusColors.error,
    });
  }

  if (suite.domains_result) {
    const r = suite.domains_result;
    cards.push({
      title: t("detector.summary.domainAccess"),
      value:
        r.blocked_count > 0
          ? t("detector.summary.blocked", { count: r.blocked_count })
          : t("detector.summary.allOk"),
      subtitle: r.summary,
      icon: <DomainIcon sx={{ fontSize: 28 }} />,
      color: r.blocked_count > 0 ? statusColors.error : statusColors.ok,
    });
  }

  if (suite.tcp_result) {
    const r = suite.tcp_result;
    cards.push({
      title: t("detector.summary.tspuDetection"),
      value: r.detected_count > 0 ? t("detector.summary.detected", { count: r.detected_count }) : t("detector.summary.clean"),
      subtitle: r.summary,
      icon: <NetworkIcon sx={{ fontSize: 28 }} />,
      color: r.detected_count > 0 ? statusColors.error : statusColors.ok,
    });
  }

  if (suite.sni_result) {
    const r = suite.sni_result;
    cards.push({
      title: t("detector.summary.sniBruteForce"),
      value:
        r.found_count > 0
          ? t("detector.summary.found", { count: r.found_count })
          : r.tested_count > 0
            ? t("detector.summary.noneFound")
            : t("detector.summary.notBlocked"),
      subtitle: r.summary,
      icon: <SniIcon sx={{ fontSize: 28 }} />,
      color:
        r.found_count > 0
          ? statusColors.ok
          : r.tested_count > 0
            ? statusColors.warning
            : statusColors.ok,
    });
  }

  if (suite.telegram_result) {
    const r = suite.telegram_result;
    const ok = r.verdict === "ok";
    let color: string = statusColors.warning;
    if (ok) color = statusColors.ok;
    else if (r.verdict === "blocked" || r.verdict === "error") color = statusColors.error;
    cards.push({
      title: t("detector.summary.telegram"),
      value: t(`detector.telegramVerdict.${r.verdict}`),
      subtitle: r.summary,
      icon: <ConnectionIcon sx={{ fontSize: 28 }} />,
      color,
    });
  }

  if (cards.length === 0) return null;

  return (
    <motion.div
      variants={staggerContainer}
      initial="hidden"
      animate="visible"
    >
      <Grid container spacing={2}>
        {cards.map((card) => (
          <Grid key={card.title} size={{ xs: 12, sm: 6, md: cards.length <= 2 ? 6 : 3 }}>
            <motion.div variants={staggerItem}>
              <SummaryCard {...card} />
            </motion.div>
          </Grid>
        ))}
      </Grid>
    </motion.div>
  );
}

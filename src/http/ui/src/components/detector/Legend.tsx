import { useState } from "react";
import { Box, Grid, Stack, Typography } from "@mui/material";
import { motion, AnimatePresence } from "motion/react";
import { InfoIcon, ExpandIcon } from "@b4.icons";
import { colors, spacing } from "@design";
import { B4Card } from "@common/B4Card";
import { StatusChip } from "./StatusChip";
import { useTranslation } from "react-i18next";

const legendGroups: { titleKey: string; statuses: string[] }[] = [
  {
    titleKey: "detector.legend.groups.dns",
    statuses: [
      "OK",
      "DNS_SPOOFING",
      "DNS_INTERCEPTION",
      "FAKE_IP",
      "FAKE_NXDOMAIN",
      "FAKE_EMPTY",
      "DOH_BLOCKED",
      "BOTH_UNAVAILABLE",
    ],
  },
  {
    titleKey: "detector.legend.groups.tls",
    statuses: [
      "TLS_DPI",
      "TLS_SPOOF",
      "TLS_ALERT",
      "TLS_RST",
      "TLS_DROP",
      "SYN_DROP",
      "TLS_MITM",
      "TCP16",
      "ISP_PAGE",
    ],
  },
  {
    titleKey: "detector.legend.groups.tcp",
    statuses: ["OK", "DETECTED", "TIMEOUT", "ERROR"],
  },
  {
    titleKey: "detector.legend.groups.sni",
    statuses: ["FOUND", "NOT_FOUND", "NOT_BLOCKED"],
  },
  {
    titleKey: "detector.legend.groups.telegram",
    statuses: ["ok", "slow", "stalled", "partial", "blocked", "error"],
  },
];

export function Legend() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  return (
    <B4Card variant="outlined" sx={{ overflow: "hidden" }}>
      <Box
        sx={{
          p: spacing.md,
          cursor: "pointer",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          "&:hover": { bgcolor: colors.accent.primaryStrong },
        }}
        onClick={() => setOpen((v) => !v)}
      >
        <Stack direction="row" alignItems="center" spacing={1.5}>
          <InfoIcon sx={{ color: colors.secondary, fontSize: 20 }} />
          <Typography variant="body2" sx={{ fontWeight: 600, color: colors.text.primary }}>
            {t("detector.legend.title")}
          </Typography>
        </Stack>
        <motion.div animate={{ rotate: open ? 180 : 0 }} transition={{ duration: 0.2 }} style={{ display: "flex" }}>
          <ExpandIcon sx={{ color: colors.text.secondary, fontSize: 18 }} />
        </motion.div>
      </Box>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.25 }}
            style={{ overflow: "hidden" }}
          >
            <Box
              sx={{
                px: spacing.md,
                pb: spacing.md,
                pt: spacing.sm,
                borderTop: `1px solid ${colors.border.light}`,
              }}
            >
              <Grid container spacing={2}>
                {legendGroups.map((group) => (
                  <Grid key={group.titleKey} size={{ xs: 12, sm: 6, md: 4 }}>
                    <Typography
                      variant="caption"
                      sx={{
                        color: colors.secondary,
                        textTransform: "uppercase",
                        letterSpacing: "0.5px",
                        fontWeight: 600,
                        display: "block",
                        mb: 0.75,
                      }}
                    >
                      {t(group.titleKey)}
                    </Typography>
                    <Stack spacing={0.6}>
                      {group.statuses.map((s) => (
                        <Stack key={s} direction="row" spacing={1} alignItems="center">
                          <Box sx={{ minWidth: 116, flexShrink: 0 }}>
                            <StatusChip status={s} />
                          </Box>
                          <Typography
                            variant="caption"
                            sx={{
                              color: colors.text.secondary,
                              fontSize: "0.7rem",
                              lineHeight: 1.25,
                            }}
                          >
                            {t(`detector.legend.meanings.${s}`)}
                          </Typography>
                        </Stack>
                      ))}
                    </Stack>
                  </Grid>
                ))}
              </Grid>
            </Box>
          </motion.div>
        )}
      </AnimatePresence>
    </B4Card>
  );
}

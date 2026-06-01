import { useState } from "react";
import { Box, Grid, Stack, Typography } from "@mui/material";
import { motion } from "motion/react";
import { DnsIcon, DomainIcon, NetworkIcon, SniIcon, SpeedIcon, ConnectionIcon } from "@b4.icons";
import { colors, radius, spacing } from "@design";
import { B4Card } from "@common/B4Card";
import { B4Switch } from "@b4.fields";
import type { DetectorTestType } from "@models/detector";
import {
  getTestName,
  getTestDescription,
  testSequence,
  staggerContainer,
  staggerItem,
} from "./constants";
import { useTranslation } from "react-i18next";

const testIcons: Record<DetectorTestType, React.ReactNode> = {
  dns: <DnsIcon />,
  "dns-availability": <SpeedIcon />,
  domains: <DomainIcon />,
  tcp: <NetworkIcon />,
  sni: <SniIcon />,
  telegram: <ConnectionIcon />,
};

interface TestSelectionGridProps {
  selectedTests: Record<DetectorTestType, boolean>;
  onToggle: (test: DetectorTestType, checked: boolean) => void;
}

function TestCard({
  test,
  selected,
  onToggle,
  name,
  description,
}: {
  test: DetectorTestType;
  selected: boolean;
  onToggle: (test: DetectorTestType, checked: boolean) => void;
  name: string;
  description: string;
}) {
  const [hovered, setHovered] = useState(false);

  return (
    <motion.div
      whileHover={{ scale: 1.02 }}
      transition={{ type: "spring", stiffness: 400, damping: 25 }}
      onHoverStart={() => setHovered(true)}
      onHoverEnd={() => setHovered(false)}
    >
      <B4Card
        variant="outlined"
        sx={{
          border: `1px solid ${selected ? colors.secondary : colors.border.light}`,
          boxShadow: selected
            ? "0 0 12px rgba(245, 173, 24, 0.3)"
            : "none",
          transition: "border-color 0.2s ease, box-shadow 0.2s ease",
          cursor: "pointer",
          userSelect: "none",
        }}
        onClick={() => onToggle(test, !selected)}
      >
        <Box sx={{ p: spacing.md }}>
          <Stack
            direction="row"
            alignItems="flex-start"
            justifyContent="space-between"
          >
            <Stack direction="row" spacing={1.5} alignItems="flex-start" sx={{ flex: 1 }}>
              <Box
                sx={{
                  p: 1.5,
                  borderRadius: radius.lg,
                  bgcolor: selected
                    ? colors.accent.secondary
                    : colors.accent.primary,
                  color: selected ? colors.secondary : colors.primary,
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  transition: "all 0.2s ease",
                }}
              >
                <motion.div
                  animate={
                    hovered
                      ? { scale: [1, 1.15, 1] }
                      : { scale: 1 }
                  }
                  transition={
                    hovered
                      ? { duration: 0.6, repeat: Infinity, repeatDelay: 0.3 }
                      : { duration: 0.2 }
                  }
                  style={{ display: "flex" }}
                >
                  {testIcons[test]}
                </motion.div>
              </Box>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography
                  variant="body2"
                  sx={{ fontWeight: 600, color: colors.text.primary }}
                >
                  {name}
                </Typography>
                <Typography
                  variant="caption"
                  sx={{
                    color: colors.text.secondary,
                    display: "block",
                    mt: 0.25,
                    lineHeight: 1.4,
                  }}
                >
                  {description}
                </Typography>
              </Box>
            </Stack>
            <Box
              onClick={(e) => e.stopPropagation()}
              sx={{ ml: 1, flexShrink: 0 }}
            >
              <B4Switch
                label=""
                checked={selected}
                onChange={(checked) => onToggle(test, checked)}
              />
            </Box>
          </Stack>
        </Box>
      </B4Card>
    </motion.div>
  );
}

export function TestSelectionGrid({
  selectedTests,
  onToggle,
}: TestSelectionGridProps) {
  const { t } = useTranslation();

  return (
    <motion.div
      variants={staggerContainer}
      initial="hidden"
      animate="visible"
    >
      <Grid container spacing={2}>
        {testSequence.map((test) => (
          <Grid key={test} size={{ xs: 12, sm: 6 }}>
            <motion.div variants={staggerItem}>
              <TestCard
                test={test}
                selected={selectedTests[test]}
                onToggle={onToggle}
                name={getTestName(t, test)}
                description={getTestDescription(t, test)}
              />
            </motion.div>
          </Grid>
        ))}
      </Grid>
    </motion.div>
  );
}

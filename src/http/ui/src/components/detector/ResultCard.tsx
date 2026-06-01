import { useState } from "react";
import { Box, Stack, Typography } from "@mui/material";
import { motion, AnimatePresence } from "motion/react";
import { ExpandIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Card } from "@common/B4Card";
import { statusColors } from "./constants";

interface ResultCardProps {
  status: "ok" | "error" | "warning";
  title: React.ReactNode;
  subtitle?: string;
  badge?: React.ReactNode;
  expandedContent?: React.ReactNode;
  index: number;
}

export function ResultCard({
  status,
  title,
  subtitle,
  badge,
  expandedContent,
  index,
}: ResultCardProps) {
  const [expanded, setExpanded] = useState(false);
  const borderColor =
    status === "ok"
      ? statusColors.ok
      : status === "error"
        ? statusColors.error
        : statusColors.warning;

  const hasExpand = !!expandedContent;

  return (
    <motion.div
      initial={{ opacity: 0, y: 15 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, delay: index * 0.04 }}
    >
      <B4Card
        variant="outlined"
        sx={{
          borderLeft: `3px solid ${borderColor}`,
          cursor: hasExpand ? "pointer" : "default",
          transition: "background-color 0.15s ease",
          "&:hover": hasExpand
            ? { bgcolor: colors.accent.primaryStrong }
            : {},
        }}
        onClick={hasExpand ? () => setExpanded((v) => !v) : undefined}
      >
        <Box sx={{ px: 2, py: 1.25 }}>
          <Stack
            direction="row"
            alignItems="center"
            justifyContent="space-between"
            spacing={1}
          >
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography
                variant="body2"
                sx={{
                  fontWeight: 600,
                  color: colors.text.primary,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {title}
              </Typography>
              {subtitle && (
                <Typography
                  variant="caption"
                  sx={{
                    color: colors.text.secondary,
                    fontFamily: "monospace",
                    fontSize: "0.75rem",
                  }}
                >
                  {subtitle}
                </Typography>
              )}
            </Box>
            <Stack direction="row" alignItems="center" spacing={1}>
              {badge}
              {hasExpand && (
                <motion.div
                  animate={{ rotate: expanded ? 180 : 0 }}
                  transition={{ duration: 0.2 }}
                  style={{ display: "flex" }}
                >
                  <ExpandIcon
                    sx={{ color: colors.text.secondary, fontSize: 18 }}
                  />
                </motion.div>
              )}
            </Stack>
          </Stack>
        </Box>
        <AnimatePresence initial={false}>
          {expanded && expandedContent && (
            <motion.div
              key="detail"
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: "auto", opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{ duration: 0.2, ease: "easeInOut" }}
              style={{ overflow: "hidden" }}
            >
              <Box
                sx={{
                  px: 2,
                  pb: 1.5,
                  pt: 0.5,
                  borderTop: `1px solid ${colors.border.light}`,
                }}
                onClick={(e) => e.stopPropagation()}
              >
                {expandedContent}
              </Box>
            </motion.div>
          )}
        </AnimatePresence>
      </B4Card>
    </motion.div>
  );
}

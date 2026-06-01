import { Box, Stack, Typography } from "@mui/material";
import { motion } from "motion/react";
import { CheckCircleIcon, ErrorIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import type { DetectorTestType, DetectorSuite } from "@models/detector";
import { getTestName, statusColors } from "./constants";
import { useTranslation } from "react-i18next";

interface ProgressGaugeProps {
  progress: number;
  currentTest?: DetectorTestType;
  completedChecks: number;
  totalChecks: number;
  tests: DetectorTestType[];
  suite: DetectorSuite;
}

const RADIUS = 54;
const STROKE = 7;
const SIZE = 140;
const CENTER = SIZE / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

const resultKeyMap: Record<DetectorTestType, keyof DetectorSuite> = {
  dns: "dns_result",
  "dns-availability": "dnsavail_result",
  domains: "domains_result",
  tcp: "tcp_result",
  sni: "sni_result",
  telegram: "telegram_result",
};

function getStepState(
  test: DetectorTestType,
  suite: DetectorSuite,
): "completed" | "running" | "failed" | "pending" {
  if (suite[resultKeyMap[test]]) return "completed";
  if (suite.current_test === test) {
    if (suite.status === "failed") return "failed";
    return "running";
  }
  return "pending";
}

function RadialGauge({
  progress,
  checksLabel,
}: Readonly<{
  progress: number;
  checksLabel: string;
}>) {
  const offset = CIRCUMFERENCE - (progress / 100) * CIRCUMFERENCE;

  return (
    <Box
      sx={{ position: "relative", width: SIZE, height: SIZE, flexShrink: 0 }}
    >
      <svg viewBox={`0 0 ${SIZE} ${SIZE}`} width={SIZE} height={SIZE}>
        {/* Background track */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={RADIUS}
          fill="none"
          stroke={colors.background.dark}
          strokeWidth={STROKE}
        />
        {/* Progress arc */}
        <motion.circle
          cx={CENTER}
          cy={CENTER}
          r={RADIUS}
          fill="none"
          stroke={colors.secondary}
          strokeWidth={STROKE}
          strokeLinecap="round"
          strokeDasharray={CIRCUMFERENCE}
          animate={{ strokeDashoffset: offset }}
          transition={{ duration: 0.5, ease: "easeOut" }}
          style={{ transform: "rotate(-90deg)", transformOrigin: "center" }}
        />
        {/* Percentage text */}
        <text
          x={CENTER}
          y={CENTER - 8}
          textAnchor="middle"
          dominantBaseline="middle"
          fill={colors.text.primary}
          fontSize="26"
          fontWeight="600"
          fontFamily="inherit"
        >
          {progress.toFixed(0)}%
        </text>
        {/* Checks count */}
        <text
          x={CENTER}
          y={CENTER + 16}
          textAnchor="middle"
          dominantBaseline="middle"
          fill={colors.text.secondary}
          fontSize="11"
          fontFamily="inherit"
        >
          {checksLabel}
        </text>
      </svg>
    </Box>
  );
}

function StepIndicator({
  state,
  label,
}: Readonly<{
  test: DetectorTestType;
  state: "completed" | "running" | "failed" | "pending";
  label: string;
}>) {
  const bgMap = {
    completed: `${statusColors.ok}33`,
    running: `${colors.secondary}33`,
    failed: `${statusColors.error}33`,
    pending: colors.background.dark,
  };
  const borderMap = {
    completed: statusColors.ok,
    running: colors.secondary,
    failed: statusColors.error,
    pending: colors.border.light,
  };

  return (
    <Stack alignItems="center" spacing={0.5} sx={{ minWidth: 64 }}>
      <motion.div
        animate={
          state === "running"
            ? {
                boxShadow: [
                  `0 0 0px ${colors.secondary}4D`,
                  `0 0 14px ${colors.secondary}99`,
                  `0 0 0px ${colors.secondary}4D`,
                ],
              }
            : state === "completed"
              ? { scale: [1, 1.15, 1] }
              : {}
        }
        transition={
          state === "running"
            ? { duration: 1.5, repeat: Infinity, ease: "easeInOut" }
            : state === "completed"
              ? { duration: 0.3 }
              : {}
        }
        style={{
          width: 32,
          height: 32,
          borderRadius: "50%",
          backgroundColor: bgMap[state],
          border: `2px solid ${borderMap[state]}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          opacity: state === "pending" ? 0.4 : 1,
        }}
      >
        {state === "completed" && (
          <CheckCircleIcon sx={{ fontSize: 18, color: statusColors.ok }} />
        )}
        {state === "failed" && (
          <ErrorIcon sx={{ fontSize: 18, color: statusColors.error }} />
        )}
        {state === "running" && (
          <Box
            sx={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              bgcolor: colors.secondary,
            }}
          />
        )}
        {state === "pending" && (
          <Box
            sx={{
              width: 6,
              height: 6,
              borderRadius: "50%",
              bgcolor: colors.text.secondary,
              opacity: 0.4,
            }}
          />
        )}
      </motion.div>
      <Typography
        variant="caption"
        sx={{
          color:
            state === "pending" ? colors.text.disabled : colors.text.secondary,
          fontSize: "0.65rem",
          textAlign: "center",
          lineHeight: 1.2,
          fontWeight: state === "running" ? 600 : 400,
        }}
      >
        {label}
      </Typography>
    </Stack>
  );
}

function Connector({
  state,
}: Readonly<{ state: "completed" | "active" | "pending" }>) {
  const colorMap = {
    completed: statusColors.ok,
    active: colors.secondary,
    pending: colors.border.light,
  };
  return (
    <Box
      sx={{
        flex: 1,
        height: 2,
        bgcolor: colorMap[state],
        opacity: state === "pending" ? 0.3 : 0.6,
        mt: "-12px",
        minWidth: 16,
      }}
    />
  );
}

export function ProgressGauge({
  progress,
  currentTest,
  completedChecks,
  totalChecks,
  tests,
  suite,
}: Readonly<ProgressGaugeProps>) {
  const { t } = useTranslation();

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 4,
        p: 2,
        borderRadius: 2,
        bgcolor: colors.accent.primaryStrong,
        border: `1px solid ${colors.border.light}`,
      }}
    >
      <RadialGauge
        progress={progress}
        checksLabel={t("detector.progress.checks", {
          completed: completedChecks,
          total: totalChecks,
        })}
      />

      <Box sx={{ flex: 1, minWidth: 0 }}>
        {currentTest && (
          <Box sx={{ mb: 2 }}>
            <Typography
              variant="caption"
              sx={{
                color: colors.text.secondary,
                textTransform: "uppercase",
                letterSpacing: "0.5px",
              }}
            >
              {t("detector.progress.currentlyRunning")}
            </Typography>
            <Box sx={{ mt: 0.5 }}>
              <B4Badge
                label={getTestName(t, currentTest)}
                size="small"
                color="primary"
              />
            </Box>
          </Box>
        )}

        {/* Step timeline */}
        <Box
          sx={{
            display: "flex",
            alignItems: "flex-start",
            gap: 0,
          }}
        >
          {tests.map((test, i) => {
            const state = getStepState(test, suite);
            const prevState = i > 0 ? getStepState(tests[i - 1], suite) : null;
            const connectorState =
              prevState === "completed"
                ? "completed"
                : prevState === "running"
                  ? "active"
                  : "pending";

            return (
              <Box
                key={test}
                sx={{
                  display: "flex",
                  alignItems: "flex-start",
                  flex: i < tests.length - 1 ? 1 : "none",
                }}
              >
                <StepIndicator
                  test={test}
                  state={state}
                  label={getTestName(t, test)}
                />
                {i < tests.length - 1 && <Connector state={connectorState} />}
              </Box>
            );
          })}
        </Box>
      </Box>
    </Box>
  );
}

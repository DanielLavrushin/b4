import { Box, Chip, Link, Stack, Typography } from "@mui/material";
import CheckIcon from "@mui/icons-material/CheckCircleOutline";
import ErrorIcon from "@mui/icons-material/ErrorOutline";
import WarningIcon from "@mui/icons-material/WarningAmber";
import InfoIcon from "@mui/icons-material/InfoOutlined";
import MonitorHeartIcon from "@mui/icons-material/MonitorHeartOutlined";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { HealthView, IssueView } from "@/models/api";
import { Section } from "@/shared/components/Section";
import { Facts } from "@/shared/components/Facts";
import { SetRef } from "@/shared/components/SetRef";
import { formatAgo, formatStamp } from "@/shared/utils/format";

const icons = {
  error: <ErrorIcon fontSize="small" sx={{ color: colors.state.error }} />,
  warning: <WarningIcon fontSize="small" sx={{ color: colors.state.warning }} />,
  info: <InfoIcon fontSize="small" sx={{ color: colors.state.info }} />,
};

const targets: Record<string, string> = {
  not_published: "/catalogue",
  build_failed: "/catalogue",
  build_overdue: "/catalogue",
  manifest_expiring: "/catalogue",
  queue_old: "/queue",
  mirror_failing: "/mirrors",
  mirror_stale: "/mirrors",
  mirror_pending: "/mirrors",
  reports_open: "/feedback?tab=reports",
};

export const severityColor = (worst: string) =>
  worst === "error" ? colors.state.error : worst === "warning" ? colors.state.warning : worst === "info" ? colors.state.info : colors.state.success;

function Issue({ issue }: Readonly<{ issue: IssueView }>) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const target = targets[issue.code];
  return (
    <Box sx={{ display: "flex", alignItems: "flex-start", gap: 1 }}>
      {icons[issue.severity]}
      <Typography variant="body2" sx={{ flex: 1 }}>
        {t(`health.issue.${issue.code}`, { ...(issue.params ?? {}), defaultValue: issue.code })}
      </Typography>
      {target && (
        <Link component="button" type="button" variant="body2" underline="hover" onClick={() => void navigate(target)}>
          {t("health.open")}
        </Link>
      )}
    </Box>
  );
}

export function HealthCard({ health }: Readonly<{ health: HealthView }>) {
  const { t } = useTranslation();
  const days = health.manifest.expires_in_s ? Math.floor(health.manifest.expires_in_s / 86400) : 0;
  return (
    <Section
      title={t("health.title")}
      description={t("health.desc")}
      icon={<MonitorHeartIcon />}
      action={<Chip size="small" variant="outlined" sx={{ borderColor: severityColor(health.worst), color: severityColor(health.worst) }} label={t(`health.worst.${health.worst}`)} />}
    >
      {health.issues.length === 0 ? (
        <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
          <CheckIcon fontSize="small" sx={{ color: colors.state.success }} />
          <Typography variant="body2">{t("health.ok")}</Typography>
        </Box>
      ) : (
        <Stack spacing={1}>
          {health.issues.map((issue) => (
            <Issue key={issue.code} issue={issue} />
          ))}
        </Stack>
      )}
      <Facts
        items={[
          {
            label: t("health.fact.expires"),
            value: health.manifest.published ? `${formatStamp(health.manifest.expires_at)} (${t("health.inDays", { count: days })})` : t("overview.notPublished"),
          },
          {
            label: t("health.fact.lastBuild"),
            value: health.build.last_ok ? `${formatAgo(t, health.build.last_ok.finished_at)} · seq ${String(health.build.last_ok.seq ?? 0)}` : t("build.never"),
          },
          {
            label: t("health.fact.lastError"),
            value: health.build.last_error ? `${formatAgo(t, health.build.last_error.finished_at)}: ${health.build.last_error.error ?? ""}` : t("health.none"),
          },
          {
            label: t("health.fact.oldestPending"),
            value: health.pending.oldest ? (
              <span>
                <SetRef id={health.pending.oldest.set_id} version={health.pending.oldest.version} title={health.pending.oldest.title} status="pending" />{" "}
                <Typography component="span" variant="caption" sx={{ color: colors.text.secondary }}>
                  {t("health.waiting", { when: formatAgo(t, health.pending.since) })}
                </Typography>
              </span>
            ) : (
              t("health.queueEmpty")
            ),
          },
          {
            label: t("health.fact.mirrors"),
            value: t("health.mirrorsLine", {
              announced: health.mirrors.announced,
              approved: health.mirrors.approved,
              failing: health.mirrors.failing,
              stale: health.mirrors.stale,
            }),
          },
        ]}
      />
    </Section>
  );
}

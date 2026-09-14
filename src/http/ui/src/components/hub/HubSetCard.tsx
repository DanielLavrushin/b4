import { useMemo } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Badge } from "@b4.elements";
import { B4Card } from "@common/B4Card";
import {
  AppliedIcon,
  DownloadIcon,
  EditIcon,
  InfoIcon,
  ReportIcon,
  TestIcon,
  ThumbDownIcon,
  ThumbDownOutlinedIcon,
  ThumbUpIcon,
  ThumbUpOutlinedIcon,
} from "@b4.icons";
import { colors, spacing, typography } from "@design";
import { HubSet, HubVoteKind, projectionToSet } from "@models/hub";
import { formatTimeAgo } from "@utils";
import { StrategySummary } from "@components/discovery/StrategySummary";
import {
  appliedAction,
  flagLabel,
  matchText,
  reportsText,
  scorePercent,
  shortAuthor,
  targetsSummary,
  voteTooltip,
} from "./text";

interface HubSetCardProps {
  set: HubSet;
  busy: boolean;
  onApply: (set: HubSet) => void;
  onUpdate: (set: HubSet) => void;
  onDetails: (set: HubSet) => void;
  onVote: (set: HubSet, kind: HubVoteKind) => void;
  onTest: (set: HubSet) => void;
  onReport: (set: HubSet) => void;
  onOpenLocal: (localSetId: string) => void;
}

export const HubSetCard = ({
  set,
  busy,
  onApply,
  onUpdate,
  onDetails,
  onVote,
  onTest,
  onReport,
  onOpenLocal,
}: HubSetCardProps) => {
  const { t } = useTranslation();
  const localSet = useMemo(
    () => projectionToSet(set.set, set.title),
    [set.set, set.title],
  );
  const percent = scorePercent(set.display);
  const applied = set.applied;
  const modified = applied?.hub_state === "modified";
  const action = appliedAction(set);
  const newest = set.display.newest
    ? formatTimeAgo(t, set.display.newest)
    : "";
  const match = matchText(t, set);

  const meta = [
    t("hub.card.by", { author: shortAuthor(set.author) }),
    t("hub.card.version", { version: set.version }),
    set.family
      ? t(`discovery.familyNames.${set.family}`, { defaultValue: set.family })
      : "",
    t("hub.card.needsB4", { version: set.b4_min }),
  ].filter(Boolean);

  return (
    <B4Card variant="outlined">
      <Box sx={{ px: spacing.md, pt: 1.5, pb: 1 }}>
        <Stack
          direction="row"
          alignItems="flex-start"
          justifyContent="space-between"
          spacing={spacing.md}
          useFlexGap
          flexWrap="wrap"
        >
          <Box sx={{ flex: "1 1 260px", minWidth: 0 }}>
            <Typography
              component="div"
              sx={{
                fontWeight: 600,
                fontSize: "0.95rem",
                color: colors.text.primary,
                overflowWrap: "anywhere",
              }}
            >
              {set.title}
            </Typography>
            <Typography
              component="div"
              variant="caption"
              sx={{ color: colors.text.secondary, mt: "2px" }}
            >
              {meta.join(" · ")}
            </Typography>
            <Typography
              component="div"
              variant="body2"
              sx={{
                ...typography.recipes.monoSmall,
                fontSize: typography.sizes.sm,
                color: colors.text.primary,
                mt: 0.75,
                overflowWrap: "anywhere",
              }}
            >
              {targetsSummary(t, set.targets)}
            </Typography>
            {match && (
              <Typography
                component="div"
                variant="caption"
                sx={{ color: colors.secondary, mt: "2px" }}
              >
                {match}
              </Typography>
            )}
          </Box>
          <Stack
            direction="row"
            spacing={0.75}
            useFlexGap
            flexWrap="wrap"
            sx={{ flexShrink: 0, alignItems: "center" }}
          >
            {applied && (
              <Tooltip title={t("hub.card.openLocal")}>
                <B4Badge
                  icon={<AppliedIcon sx={{ fontSize: 14 }} />}
                  label={
                    modified
                      ? t("hub.card.appliedModified")
                      : t("hub.card.applied")
                  }
                  color="success"
                  variant={modified ? "outlined" : "filled"}
                  onClick={() => onOpenLocal(applied.set_id)}
                />
              </Tooltip>
            )}
            {set.flags.map((flag) => (
              <B4Badge
                key={flag}
                label={flagLabel(t, flag)}
                variant="outlined"
                color={flag === "block" ? "error" : "default"}
              />
            ))}
          </Stack>
        </Stack>

        {set.description && (
          <Typography
            variant="body2"
            sx={{ color: colors.text.secondary, mt: 1 }}
          >
            {set.description}
          </Typography>
        )}

        <Box
          sx={{
            display: "flex",
            gap: 2,
            alignItems: "flex-start",
            flexWrap: "wrap",
            mt: 1.5,
            p: 1.5,
            border: `1px solid ${colors.border.light}`,
            borderRadius: 1.5,
            bgcolor: colors.background.dark,
          }}
        >
          <Box sx={{ flex: "1 1 320px", minWidth: 0 }}>
            <StrategySummary
              set={localSet}
              domains={set.targets.domains ?? []}
              compact
            />
          </Box>
          <Stack spacing={0.25} sx={{ flex: "0 1 200px", minWidth: 160 }}>
            <Typography
              component="div"
              sx={{
                ...typography.recipes.metricLabel,
                color: colors.text.disabled,
              }}
            >
              {t("hub.card.reputation")}
            </Typography>
            {percent !== null ? (
              <Typography
                component="div"
                sx={{
                  fontSize: "1.5rem",
                  fontWeight: 700,
                  lineHeight: 1.1,
                  color:
                    percent >= 60
                      ? colors.state.success
                      : percent >= 30
                        ? colors.state.warning
                        : colors.state.error,
                }}
              >
                {t("hub.card.score", { percent })}
              </Typography>
            ) : (
              <Typography
                component="div"
                sx={{ fontSize: "1rem", fontWeight: 600, color: colors.text.disabled }}
              >
                {t("hub.card.reports.none")}
              </Typography>
            )}
            <Typography variant="body2" sx={{ color: colors.text.primary }}>
              {percent !== null ? reportsText(t, set.display) : ""}
            </Typography>
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {[
                set.display.devices > 0
                  ? t("hub.card.devices", { count: set.display.devices })
                  : "",
                newest ? t("hub.card.newest", { ago: newest }) : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </Typography>
          </Stack>
        </Box>
      </Box>

      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        flexWrap="wrap"
        sx={{
          px: spacing.md,
          py: 1,
          borderTop: `1px solid ${colors.border.light}`,
          alignItems: "center",
        }}
      >
        <AppliedActionButton
          set={set}
          busy={busy}
          onApply={onApply}
          onUpdate={onUpdate}
        />
        {applied && action === "applied" && (
          <Button
            variant="outlined"
            size="small"
            startIcon={<EditIcon />}
            onClick={() => onOpenLocal(applied.set_id)}
          >
            {t("hub.apply.openSet")}
          </Button>
        )}
        <Button
          variant="outlined"
          size="small"
          startIcon={<InfoIcon />}
          onClick={() => onDetails(set)}
        >
          {t("hub.card.details")}
        </Button>
        <Button
          size="small"
          startIcon={<ReportIcon />}
          disabled={busy}
          onClick={() => onReport(set)}
          sx={{ color: colors.text.secondary }}
        >
          {t("hub.card.report")}
        </Button>
        {applied && (
          <>
            <Box sx={{ flex: 1 }} />
            <Tooltip
              title={
                modified
                  ? t("hub.card.voteModified")
                  : voteTooltip(t, applied, "works")
              }
            >
              <span>
                <Button
                  size="small"
                  startIcon={
                    applied.vote === "works" ? (
                      <ThumbUpIcon />
                    ) : (
                      <ThumbUpOutlinedIcon />
                    )
                  }
                  disabled={busy || modified}
                  onClick={() => onVote(set, "works")}
                  sx={{ color: colors.state.success }}
                >
                  {t("hub.card.works")}
                </Button>
              </span>
            </Tooltip>
            <Tooltip
              title={
                modified
                  ? t("hub.card.voteModified")
                  : voteTooltip(t, applied, "broken")
              }
            >
              <span>
                <Button
                  size="small"
                  startIcon={
                    applied.vote === "broken" ? (
                      <ThumbDownIcon />
                    ) : (
                      <ThumbDownOutlinedIcon />
                    )
                  }
                  disabled={busy || modified}
                  onClick={() => onVote(set, "broken")}
                  sx={{ color: colors.state.error }}
                >
                  {t("hub.card.broken")}
                </Button>
              </span>
            </Tooltip>
            <Button
              size="small"
              startIcon={<TestIcon />}
              disabled={busy}
              onClick={() => onTest(set)}
            >
              {t("hub.card.test")}
            </Button>
          </>
        )}
      </Stack>
    </B4Card>
  );
};

interface AppliedActionButtonProps {
  set: HubSet;
  busy: boolean;
  onApply: (set: HubSet) => void;
  onUpdate: (set: HubSet) => void;
}

export const AppliedActionButton = ({
  set,
  busy,
  onApply,
  onUpdate,
}: AppliedActionButtonProps) => {
  const { t } = useTranslation();
  const action = appliedAction(set);
  const spinner = busy ? (
    <CircularProgress size={14} color="inherit" />
  ) : null;

  if (action === "applied") {
    return (
      <Button
        variant="contained"
        size="small"
        startIcon={<AppliedIcon />}
        disabled
      >
        {t("hub.card.applied")}
      </Button>
    );
  }
  if (action === "update") {
    return (
      <Button
        variant="contained"
        size="small"
        startIcon={spinner ?? <DownloadIcon />}
        disabled={busy}
        onClick={() => onUpdate(set)}
      >
        {t("hub.card.update", { version: set.version })}
      </Button>
    );
  }
  if (action === "reapply") {
    return (
      <Button
        variant="contained"
        size="small"
        startIcon={spinner ?? <DownloadIcon />}
        disabled={busy}
        onClick={() => onUpdate(set)}
      >
        {t("hub.card.reapply")}
      </Button>
    );
  }
  return (
    <Button
      variant="contained"
      size="small"
      startIcon={spinner ?? <DownloadIcon />}
      disabled={busy}
      onClick={() => onApply(set)}
    >
      {t("hub.card.apply")}
    </Button>
  );
};

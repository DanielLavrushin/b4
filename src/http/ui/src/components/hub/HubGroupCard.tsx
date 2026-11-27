import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Badge } from "@b4.elements";
import { B4Card } from "@common/B4Card";
import { AppliedIcon, CategoryIcon, DownloadIcon, InfoIcon } from "@b4.icons";
import { colors, spacing, typography } from "@design";
import { HubGroup, HubSet, HubVoteKind } from "@models/hub";
import { hubGroupAction, hubGroupEscalationTarget } from "@models/hubGroups";
import { HubVoteActions } from "./HubSetCard";
import { reportsText, scorePercent, shortAuthor, targetsSummary } from "./text";

interface HubGroupCardProps {
  hubGroup: HubGroup;
  busy: boolean;
  onApply: (hubGroup: HubGroup) => void;
  onUpdate: (hubGroup: HubGroup) => void;
  onDetails: (hubSetId: string) => void;
  onVote: (set: HubSet, kind: HubVoteKind) => void;
  onTest: (set: HubSet) => void;
}

const scoreColor = (percent: number): string => {
  if (percent >= 60) return colors.state.success;
  if (percent >= 30) return colors.state.warning;
  return colors.state.error;
};

export const HubGroupCard = ({
  hubGroup,
  busy,
  onApply,
  onUpdate,
  onDetails,
  onVote,
  onTest,
}: HubGroupCardProps) => {
  const { t } = useTranslation();
  const action = hubGroupAction(hubGroup);
  const applied = hubGroup.applied;
  const count = hubGroup.members.length;

  const meta = [
    t("hub.card.by", { author: shortAuthor(hubGroup.author) }),
    t("hub.card.version", { version: hubGroup.version }),
  ];

  const actionLabel = {
    apply: t("hub.group.card.apply"),
    applied: t("hub.card.applied"),
    update: t("hub.group.card.update"),
    reapply: t("hub.group.card.reapply"),
  }[action];

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
            <Stack direction="row" spacing={0.75} alignItems="center">
              <CategoryIcon sx={{ fontSize: 16, color: colors.secondary }} />
              <Typography
                component="div"
                sx={{
                  fontWeight: 600,
                  fontSize: "0.95rem",
                  color: colors.text.primary,
                  overflowWrap: "anywhere",
                }}
              >
                {t("hub.group.card.title", { title: hubGroup.title, count })}
              </Typography>
            </Stack>
            <Typography
              component="div"
              variant="caption"
              sx={{ color: colors.text.secondary, mt: "2px" }}
            >
              {meta.join(" · ")}
            </Typography>
          </Box>
          {applied.state !== "none" && (
            <B4Badge
              icon={<AppliedIcon sx={{ fontSize: 14 }} />}
              label={
                applied.state === "modified"
                  ? t("hub.group.card.appliedModified", { group: applied.group })
                  : t("hub.group.card.appliedAs", { group: applied.group })
              }
              color="success"
              variant={applied.state === "modified" ? "outlined" : "filled"}
            />
          )}
        </Stack>

        {hubGroup.description && (
          <Typography
            variant="body2"
            sx={{ color: colors.text.secondary, mt: 1 }}
          >
            {hubGroup.description}
          </Typography>
        )}

        <Stack spacing={1} sx={{ mt: 1.5 }}>
          {hubGroup.members.map((member, position) => {
            const set = member.set;
            const percent = scorePercent(set.display);
            const target =
              member.escalate === undefined
                ? undefined
                : hubGroup.members[member.escalate.to]?.set.title;
            return (
              <Box
                key={`${set.id}:${position}`}
                sx={{
                  display: "flex",
                  gap: 2,
                  alignItems: "flex-start",
                  flexWrap: "wrap",
                  p: 1.25,
                  border: `1px solid ${colors.border.light}`,
                  borderRadius: 1.5,
                  bgcolor: colors.background.dark,
                }}
              >
                <Box sx={{ flex: "1 1 260px", minWidth: 0 }}>
                  <Typography
                    component="div"
                    variant="subtitle2"
                    sx={{ color: colors.text.primary, overflowWrap: "anywhere" }}
                  >
                    {set.title}
                  </Typography>
                  <Typography
                    component="div"
                    variant="body2"
                    sx={{
                      ...typography.recipes.monoSmall,
                      fontSize: typography.sizes.sm,
                      color: colors.text.primary,
                      mt: 0.5,
                      overflowWrap: "anywhere",
                    }}
                  >
                    {targetsSummary(t, set.targets)}
                  </Typography>
                  {target && (
                    <Typography
                      component="div"
                      variant="caption"
                      sx={{ color: colors.text.secondary, mt: "2px" }}
                    >
                      {t("hub.group.card.escalates", { name: target })}
                    </Typography>
                  )}
                </Box>
                <Stack
                  direction="row"
                  spacing={1.5}
                  useFlexGap
                  flexWrap="wrap"
                  alignItems="center"
                  sx={{ maxWidth: "100%" }}
                >
                  <Box sx={{ textAlign: "right" }}>
                    <Typography
                      component="div"
                      sx={{
                        fontWeight: 700,
                        color:
                          percent === null
                            ? colors.text.disabled
                            : scoreColor(percent),
                      }}
                    >
                      {percent === null
                        ? t("hub.card.reports.none")
                        : t("hub.card.score", { percent })}
                    </Typography>
                    {percent !== null && (
                      <Typography
                        component="div"
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {reportsText(t, set.display)}
                      </Typography>
                    )}
                  </Box>
                  {set.applied && (
                    <HubVoteActions
                      set={set}
                      busy={busy}
                      onVote={onVote}
                      onTest={
                        hubGroupEscalationTarget(hubGroup, position)
                          ? undefined
                          : onTest
                      }
                    />
                  )}
                  <Button
                    size="small"
                    startIcon={<InfoIcon />}
                    onClick={() => onDetails(set.id)}
                  >
                    {t("hub.card.details")}
                  </Button>
                </Stack>
              </Box>
            );
          })}
        </Stack>
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
        <Button
          variant="contained"
          size="small"
          startIcon={
            action === "applied" ? (
              <AppliedIcon />
            ) : busy ? (
              <CircularProgress size={14} color="inherit" />
            ) : (
              <DownloadIcon />
            )
          }
          disabled={busy || action === "applied"}
          onClick={() =>
            action === "apply" ? onApply(hubGroup) : onUpdate(hubGroup)
          }
        >
          {actionLabel}
        </Button>
      </Stack>
    </B4Card>
  );
};

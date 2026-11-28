import { Box, ButtonBase, Tooltip, Typography } from "@mui/material";
import CategoryIcon from "@mui/icons-material/Category";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { SetMembershipView } from "@/models/api";
import { groupPath } from "@/features/groups/GroupDrawerHost";
import { StatusChip } from "@/shared/components/StatusChip";
import { useOverlay } from "@/shared/hooks/useOverlay";

export function GroupStrip({ group }: Readonly<{ group: SetMembershipView }>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  return (
    <Box
      sx={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: "100%",
        height: 24,
        mb: "3px",
        display: "flex",
        alignItems: "center",
        gap: 0.75,
        minWidth: 0,
      }}
    >
      <CategoryIcon sx={{ fontSize: 16, color: colors.secondary }} />
      <Tooltip title={t("groups.open", { version: group.version })}>
        <ButtonBase
          onClick={() => overlay.open(groupPath(group.group_id, group.version))}
          sx={{ minWidth: 0, borderRadius: 0.5, "&:hover": { color: colors.secondary } }}
        >
          <Typography variant="body2" fontWeight={600} noWrap>
            {t("groups.heading", { title: group.title, count: group.members })}
          </Typography>
        </ButtonBase>
      </Tooltip>
      <Box sx={{ flexShrink: 0, display: "flex" }}>
        <StatusChip status={group.status} label={t(`status.set.${group.status}`)} />
      </Box>
    </Box>
  );
}

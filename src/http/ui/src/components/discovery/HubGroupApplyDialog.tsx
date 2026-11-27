import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Dialog } from "@b4.elements";
import { CategoryIcon } from "@b4.icons";
import { colors } from "@design";
import { HubGroup } from "@models/hub";
import { hubGroupAction, hubGroupTakenOver } from "@models/hubGroups";

export interface HubGroupOffer {
  hubGroup: HubGroup;
  hubSetId: string;
}

interface HubGroupApplyDialogProps {
  offer: HubGroupOffer | null;
  loading: boolean;
  onConfirm: (offer: HubGroupOffer) => void;
  onClose: () => void;
}

export const HubGroupApplyDialog = ({
  offer,
  loading,
  onConfirm,
  onClose,
}: HubGroupApplyDialogProps) => {
  const { t } = useTranslation();
  const hubGroup = offer?.hubGroup;
  const members = hubGroup?.members ?? [];
  const tested =
    members.find((m) => m.set.id === offer?.hubSetId)?.set.title ?? "";
  const untested = members
    .filter((m) => m.set.id !== offer?.hubSetId)
    .map((m) => m.set.title);
  const replace = Boolean(hubGroup && hubGroupAction(hubGroup) !== "apply");
  const takenOver = hubGroup ? hubGroupTakenOver(hubGroup) : [];

  return (
    <B4Dialog
      open={offer !== null}
      title={t("discovery.results.hubGroupTitle", {
        title: hubGroup?.title ?? "",
      })}
      icon={<CategoryIcon />}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            variant="contained"
            disabled={loading}
            startIcon={
              loading ? <CircularProgress size={16} color="inherit" /> : undefined
            }
            onClick={() => {
              if (offer) onConfirm(offer);
            }}
          >
            {t("discovery.results.hubGroupConfirm")}
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ mt: 2 }}>
        <Typography variant="body2">
          {t("discovery.results.hubGroupTested", { name: tested })}
        </Typography>
        {untested.length > 0 && (
          <Typography variant="body2" sx={{ color: colors.state.warning }}>
            {t("discovery.results.hubGroupUntested", {
              count: untested.length,
              sets: untested.join(", "),
            })}
          </Typography>
        )}
        {replace && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("discovery.results.hubGroupReplace", {
              group: hubGroup?.applied.group ?? "",
            })}
          </Typography>
        )}
        {takenOver.length > 0 && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("hub.group.apply.takeOver", {
              count: takenOver.length,
              sets: takenOver.join(", "),
            })}
          </Typography>
        )}
      </Stack>
    </B4Dialog>
  );
};

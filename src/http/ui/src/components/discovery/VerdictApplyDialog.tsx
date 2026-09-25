import { useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  CircularProgress,
  FormControlLabel,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon } from "@b4.icons";
import { B4Alert } from "@b4.elements";
import { B4Dialog } from "@common/B4Dialog";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { probeUrlLabel } from "@utils";
import { StrategySummary } from "./StrategySummary";

interface VerdictApplyDialogProps {
  set: B4SetConfig;
  strategy: B4SetConfig;
  preset: string;
  domains: string[];
  uncovered: string[];
  probeUrls: string[];
  loading: boolean;
  onClose: () => void;
  onConfirm: (useUrls: boolean) => void;
}

export const VerdictApplyDialog = ({
  set,
  strategy,
  preset,
  domains,
  uncovered,
  probeUrls,
  loading,
  onClose,
  onConfirm,
}: VerdictApplyDialogProps) => {
  const { t } = useTranslation();
  const storedUrls = set.discovery?.urls ?? [];
  const [useUrls, setUseUrls] = useState(storedUrls.length === 0);

  const captionSx = { color: colors.text.secondary, display: "block" };

  return (
    <B4Dialog
      open
      onClose={onClose}
      title={t("discovery.verdict.dialogTitle", { name: set.name })}
      subtitle={domains.join(", ")}
      icon={<AddIcon />}
      maxWidth="sm"
      fullWidth
      actions={
        <Stack direction="row" spacing={2}>
          <Button onClick={onClose} disabled={loading}>
            {t("core.cancel")}
          </Button>
          <Button
            variant="contained"
            onClick={() => onConfirm(useUrls && probeUrls.length > 0)}
            disabled={loading}
            startIcon={
              loading ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            sx={{ bgcolor: colors.secondary, color: colors.background.default }}
          >
            {t("discovery.apply.replaceAction")}
          </Button>
        </Stack>
      }
    >
      <Stack spacing={2.5} sx={{ mt: 1 }}>
        {uncovered.length > 0 && (
          <B4Alert severity="warning">
            {t("discovery.verdict.dialogUncovered", {
              domains: uncovered.join(", "),
              name: set.name,
            })}
          </B4Alert>
        )}
        <Box>
          <Typography
            variant="subtitle2"
            sx={{ mb: 1, color: colors.text.secondary }}
          >
            {t("discovery.apply.will")}
          </Typography>
          <Box
            sx={{
              p: 1.5,
              border: `1px solid ${colors.border.light}`,
              borderRadius: 1.5,
              bgcolor: colors.background.dark,
            }}
          >
            <StrategySummary
              set={strategy}
              preset={preset || undefined}
              domains={domains}
              compact
            />
          </Box>
        </Box>
        {!set.enabled && (
          <B4Alert severity="info">
            {t("discovery.apply.setDisabled", { name: set.name })}
          </B4Alert>
        )}
        {probeUrls.length > 0 && (
          <Box>
            <FormControlLabel
              control={
                <Checkbox
                  checked={useUrls}
                  onChange={(e) => setUseUrls(e.target.checked)}
                />
              }
              label={t("discovery.apply.useUrls")}
            />
            <Typography variant="caption" sx={captionSx}>
              {probeUrls.map(probeUrlLabel).join(", ")}
              {storedUrls.length > 0 &&
                ` · ${t("discovery.apply.urlsStored", { count: storedUrls.length })}`}
            </Typography>
          </Box>
        )}
        <Typography variant="caption" sx={captionSx}>
          {t("discovery.apply.replaceNote")}
          {set.hub?.id && ` ${t("discovery.apply.replaceHub")}`}
        </Typography>
      </Stack>
    </B4Dialog>
  );
};

import { useState } from "react";
import { AlertTitle, Box, Button, Stack, Typography } from "@mui/material";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { B4Alert } from "@b4.elements";
import { CoreIcon, RestartIcon, SwapIcon } from "@b4.icons";
import { colors, fonts } from "@design";
import { configApi } from "@api/settings";
import { useSnackbar } from "@context/SnackbarProvider";
import type { EngineFailure } from "@models/settings";

interface EngineFailureCardProps {
  failure: EngineFailure;
  onRestart: () => void;
}

const engineName = (mode: EngineFailure["mode"]) =>
  mode === "tun" ? "TUN" : "NFQUEUE";

export const EngineFailureCard = ({
  failure,
  onRestart,
}: EngineFailureCardProps) => {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { showError } = useSnackbar();
  const [switching, setSwitching] = useState(false);

  const target: EngineFailure["mode"] =
    failure.mode === "tun" ? "nfqueue" : "tun";

  const switchEngine = async () => {
    setSwitching(true);
    try {
      const config = await configApi.get();
      config.queue.mode = target === "tun" ? "tun" : "";
      await configApi.save(config);
      onRestart();
    } catch (error) {
      showError(
        error instanceof Error
          ? error.message
          : t("dashboard.engineFailure.switchFailed"),
      );
    } finally {
      setSwitching(false);
    }
  };

  const retryTime =
    failure.retry_at > 0
      ? new Date(failure.retry_at).toLocaleTimeString(i18n.language)
      : "";

  return (
    <B4Alert severity="error" noWrapper sx={{ mb: 1.5 }}>
      <AlertTitle>
        {t("dashboard.engineFailure.title", {
          engine: engineName(failure.mode),
        })}
      </AlertTitle>
      <Typography variant="body2">
        {t("dashboard.engineFailure.body")}
      </Typography>
      <Box
        component="pre"
        sx={{
          fontFamily: fonts.mono,
          fontSize: 12,
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          color: colors.text.primary,
          my: 1,
        }}
      >
        {failure.error}
      </Box>
      <Typography variant="caption" sx={{ display: "block" }}>
        {retryTime
          ? t("dashboard.engineFailure.retryAt", {
              time: retryTime,
              count: failure.retries_left,
            })
          : t("dashboard.engineFailure.noRetries")}
      </Typography>
      <Stack direction="row" sx={{ mt: 1.5, flexWrap: "wrap", gap: 1 }}>
        <Button
          size="small"
          variant="contained"
          startIcon={<SwapIcon />}
          disabled={switching}
          onClick={() => void switchEngine()}
        >
          {t("dashboard.engineFailure.switchTo", {
            engine: engineName(target),
          })}
        </Button>
        <Button
          size="small"
          variant="outlined"
          startIcon={<RestartIcon />}
          onClick={onRestart}
        >
          {t("dashboard.engineFailure.restart")}
        </Button>
        <Button
          size="small"
          startIcon={<CoreIcon />}
          onClick={() => {
            navigate("/settings/general/engine")?.catch(() => {});
          }}
        >
          {t("dashboard.engineFailure.settings")}
        </Button>
      </Stack>
    </B4Alert>
  );
};

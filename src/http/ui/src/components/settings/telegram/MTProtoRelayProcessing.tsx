import { useTranslation } from "react-i18next";
import { Box, Stack, Typography } from "@mui/material";
import { colors } from "@design";
import { B4Config } from "@models/config";
import {
  telegramSetId,
  telegramSetRoutes,
  telegramSetUsable,
} from "@models/telegramSet";

type RelayProcessing = "none" | "strategy" | "route" | "sets";

const relayProcessing = (
  config: B4Config,
  setsMayProcess: boolean,
  followsTelegramSetStrategy: boolean,
): RelayProcessing => {
  if (telegramSetUsable(config)) {
    if (telegramSetRoutes(config)) return "route";
    return followsTelegramSetStrategy ? "strategy" : "none";
  }
  return setsMayProcess ? "sets" : "none";
};

export const MTProtoRelayProcessing = ({ config }: { config: B4Config }) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const name =
    config.sets.find((s) => s.id === telegramSetId(config))?.name ?? "";
  const rows: { label: string; processing: RelayProcessing }[] = [];
  if (
    (mtproto?.cfproxy_enabled ?? true) ||
    (mtproto?.ws_custom_domain || "").trim() !== ""
  ) {
    rows.push({
      label: t("settings.MTProto.relayProcessingCFProxy"),
      processing: relayProcessing(config, !!mtproto?.cfproxy_dpi, true),
    });
  }
  if ((mtproto?.cfworker_domain || "").trim() !== "") {
    rows.push({
      label: t("settings.MTProto.relayProcessingWorker"),
      processing: relayProcessing(
        config,
        !!mtproto?.cfworker_dpi,
        !!mtproto?.cfworker_dpi,
      ),
    });
  }
  if (rows.length === 0) return null;

  const valueText = (processing: RelayProcessing) => {
    switch (processing) {
      case "strategy":
        return t("settings.MTProto.relayProcessingStrategy", { name });
      case "route":
        return t("settings.MTProto.relayProcessingRoute", { name });
      case "sets":
        return t("settings.MTProto.relayProcessingSets");
      default:
        return t("settings.MTProto.relayProcessingNone");
    }
  };

  return (
    <Box
      sx={{
        border: `1px solid ${colors.border.light}`,
        borderRadius: 1,
        px: 1.5,
        py: 1,
      }}
    >
      <Typography
        variant="caption"
        sx={{ color: colors.text.secondary, display: "block", mb: 0.5 }}
      >
        {t("settings.MTProto.relayProcessingTitle")}
      </Typography>
      <Stack spacing={0.5}>
        {rows.map((row) => (
          <Typography key={row.label} variant="body2">
            <Box component="span" sx={{ color: colors.text.secondary }}>
              {row.label}:
            </Box>{" "}
            {valueText(row.processing)}
          </Typography>
        ))}
      </Stack>
    </Box>
  );
};

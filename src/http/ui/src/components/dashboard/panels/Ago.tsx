import { memo, type ReactNode } from "react";
import { Box, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useServerNow } from "@/stores/useMetrics";
import { agePart, formatDateTime, validTime } from "./format";

export const AGO_TICK_MS = 5_000;

export function useAgoText(at: number): string | null {
  const { t } = useTranslation();
  const now = useServerNow(AGO_TICK_MS);
  if (!validTime(at)) return null;
  const part = agePart(now - at);
  if (!part) return t("dashboard.common.justNow");
  return t("dashboard.common.ago", {
    duration: t(`charts.${part.unit}`, { n: part.count }),
  });
}

interface AgoProps {
  at: number;
  template?: (ago: string) => string;
  fallback?: ReactNode;
}

function AgoLabel({ at, template, fallback = null }: AgoProps) {
  const { i18n } = useTranslation();
  const ago = useAgoText(at);
  if (ago === null) return <>{fallback}</>;
  return (
    <Tooltip title={formatDateTime(at, i18n.language)} describeChild>
      <Box
        component="time"
        dateTime={new Date(at).toISOString()}
        sx={{ whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}
      >
        {template ? template(ago) : ago}
      </Box>
    </Tooltip>
  );
}

export const Ago = memo(AgoLabel);

import { Box, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useServerNow } from "@/stores/useMetrics";
import { formatUptime } from "../statusFormat";
import { AGO_TICK_MS } from "./Ago";
import { formatSince, validTime } from "./format";
import { numeric } from "./styles";

export function TimeLeft({ until, strings }: { until: number; strings: string }) {
  const { t, i18n } = useTranslation();
  const now = useServerNow(AGO_TICK_MS);
  if (!validTime(until)) return null;
  const left = until - now;
  const text =
    left > 0
      ? t(`${strings}.left`, {
          duration: formatUptime(Math.ceil(left / 1000), t),
        })
      : t(`${strings}.expiring`);
  return (
    <Tooltip
      title={t(`${strings}.endsAt`, {
        time: formatSince(until, now, i18n.language),
      })}
      describeChild
    >
      <Box component="span" sx={{ ...numeric, whiteSpace: "nowrap" }}>
        {text}
      </Box>
    </Tooltip>
  );
}

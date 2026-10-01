import { Alert, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { ModerationItemView, SetAction } from "@/models/api";

export const catalogueEffect = (item: ModerationItemView): { key: string; params: Record<string, unknown> } => {
  const { listed, listed_after: after } = item;
  if (listed === after) {
    return after > 0 ? { key: "moderation.effect.unchanged", params: { version: after } } : { key: "moderation.effect.none", params: {} };
  }
  if (after === 0) return { key: "moderation.effect.leaves", params: { version: listed } };
  if (listed === 0) return { key: "moderation.effect.joins", params: { version: after } };
  return { key: "moderation.effect.replaces", params: { version: after, previous: listed } };
};

export function EffectText({ item, action }: { item: ModerationItemView; action: SetAction }) {
  const { t } = useTranslation();
  if (!item.ok) {
    return <Alert severity="error">{t(`errors.${item.code ?? "invalid"}`, { ...(item.params ?? {}), defaultValue: item.code })}</Alert>;
  }
  const effect = catalogueEffect(item);
  const leaves = item.listed > 0 && item.listed_after === 0;
  const replaces = item.listed > 0 && item.listed_after > 0 && item.listed !== item.listed_after;
  return (
    <Alert severity={leaves ? "warning" : replaces ? "info" : "success"} variant="outlined">
      <Stack spacing={0.5}>
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {t(effect.key, effect.params)}
        </Typography>
        {item.withheld && item.listed_after === 0 && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t(`moderation.effect.withheld.${item.withheld}`)}
          </Typography>
        )}
        {item.reports > 0 && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t(action === "restore" ? "moderation.effect.reportsDismissed" : "moderation.effect.reportsResolved", { count: item.reports })}
          </Typography>
        )}
      </Stack>
    </Alert>
  );
}

import { Alert, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { ModerationItemView, SetAction } from "@/models/api";

export const catalogueEffect = (
  item: Pick<ModerationItemView, "listed" | "listed_after">,
  base = "moderation.effect",
): { key: string; params: Record<string, unknown> } => {
  const { listed, listed_after: after } = item;
  if (listed === after) {
    return after > 0 ? { key: `${base}.unchanged`, params: { version: after } } : { key: `${base}.none`, params: {} };
  }
  if (after === 0) return { key: `${base}.leaves`, params: { version: listed } };
  if (listed === 0) return { key: `${base}.joins`, params: { version: after } };
  return { key: `${base}.replaces`, params: { version: after, previous: listed } };
};

export function EffectText({ item, action }: { item: ModerationItemView; action: SetAction }) {
  const { t } = useTranslation();
  if (!item.ok) {
    return <Alert severity="error">{t(`errors.${item.code ?? "invalid"}`, { ...(item.params ?? {}), defaultValue: item.code })}</Alert>;
  }
  const effect = catalogueEffect(item);
  const leaves = item.listed > 0 && item.listed_after === 0;
  return (
    <Alert severity={leaves ? "warning" : "info"}>
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

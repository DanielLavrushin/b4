import { Box, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { ErrorState } from "@/shared/components/States";
import { setRef } from "@/shared/utils/format";
import { useKeyImpact } from "./api";

export function KeyImpactSummary({ keyHmac, mode }: { keyHmac: string; mode: "ban" | "unban" }) {
  const { t } = useTranslation();
  const impact = useKeyImpact(keyHmac);
  if (impact.isLoading) return <CircularProgress size={18} />;
  if (impact.error) return <ErrorState error={impact.error} onRetry={() => void impact.refetch()} />;
  const data = impact.data;
  if (!data) return null;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {t(`keys.impact.${mode}.listed`, { count: data.listed.length })}
      </Typography>
      {data.listed.length > 0 && (
        <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
          {data.listed.slice(0, 12).map((v) => (
            <li key={v.set_id}>
              <Typography variant="body2" component="span">
                {v.title}
              </Typography>{" "}
              <Typography variant="caption" component="span" sx={{ color: colors.text.secondary }}>
                {setRef(v.set_id, v.version)}
              </Typography>
            </li>
          ))}
          {data.listed.length > 12 && <li>{t("app.andMore", { count: data.listed.length - 12 })}</li>}
        </Stack>
      )}
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t(`keys.impact.${mode}.pending`, { count: data.pending.length })}
      </Typography>
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t(`keys.impact.${mode}.votes`, { count: data.votes, sets: data.voted_sets })}
      </Typography>
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t(`keys.impact.${mode}.reports`, { count: data.reports })}
      </Typography>
      {data.mirrors.length > 0 && (
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("keys.impact.mirrors", { count: data.mirrors.length })}
        </Typography>
      )}
    </Box>
  );
}

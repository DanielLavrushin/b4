import { useEffect, useState } from "react";
import { Box, Button, Grid, InputAdornment, Stack, TextField, Typography } from "@mui/material";
import TuneIcon from "@mui/icons-material/TuneOutlined";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSaveSettings, useSettings } from "@/api/hub";
import { useSnackbar } from "@/context/SnackbarProvider";
import { Section } from "@/components/common/Section";
import { ErrorState, Loading } from "@/components/common/States";
import type { LimitsView } from "@/models/api";

const MAX_LIMIT = 1_000_000;

type LimitField = keyof LimitsView;

const fields: { key: LimitField; per: "day" | "hour" }[] = [
  { key: "shares_per_day", per: "day" },
  { key: "votes_per_day", per: "day" },
  { key: "reports_per_day", per: "day" },
  { key: "mirrors_per_day", per: "day" },
  { key: "new_keys_per_day", per: "day" },
  { key: "requests_per_hour", per: "hour" },
];

type Draft = Record<LimitField, string>;

const toDraft = (limits: LimitsView): Draft => ({
  shares_per_day: String(limits.shares_per_day),
  votes_per_day: String(limits.votes_per_day),
  reports_per_day: String(limits.reports_per_day),
  mirrors_per_day: String(limits.mirrors_per_day),
  new_keys_per_day: String(limits.new_keys_per_day),
  requests_per_hour: String(limits.requests_per_hour),
});

const parseField = (raw: string): number | null => {
  const n = Number(raw.trim());
  if (!Number.isInteger(n) || n < 1 || n > MAX_LIMIT) return null;
  return n;
};

export function SettingsPage() {
  const { t } = useTranslation();
  const settings = useSettings();
  const save = useSaveSettings();
  const { notify, notifyError } = useSnackbar();
  const [draft, setDraft] = useState<Draft | null>(null);

  useEffect(() => {
    if (settings.data && draft === null) setDraft(toDraft(settings.data.limits));
  }, [settings.data, draft]);

  if (settings.isLoading || draft === null) return <Loading />;
  if (settings.error) return <ErrorState error={settings.error} />;
  const data = settings.data;
  if (!data) return null;

  const parsed = Object.fromEntries(fields.map((f) => [f.key, parseField(draft[f.key])])) as Record<LimitField, number | null>;
  const invalid = fields.some((f) => parsed[f.key] === null);
  const dirty = fields.some((f) => parsed[f.key] !== data.limits[f.key]);

  const submit = async () => {
    if (invalid) return;
    try {
      const saved = await save.mutateAsync(parsed as LimitsView);
      setDraft(toDraft(saved.limits));
      notify(t("settings.saved"), "success");
    } catch (err) {
      notifyError(err);
    }
  };

  return (
    <Grid container spacing={2}>
      <Grid size={{ xs: 12, lg: 7 }}>
        <Section title={t("settings.limits")} description={t("settings.limitsDesc")} icon={<TuneIcon />}>
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("settings.limitsText")}
          </Typography>
          <Stack spacing={2}>
            {fields.map((f) => {
              const value = parsed[f.key];
              const isDefault = value === data.defaults[f.key];
              return (
                <TextField
                  key={f.key}
                  size="small"
                  fullWidth
                  label={t(`settings.fields.${f.key}`)}
                  helperText={
                    value === null
                      ? t("settings.invalid", { max: MAX_LIMIT })
                      : isDefault
                        ? t("settings.isDefault")
                        : t("settings.defaultIs", { value: data.defaults[f.key] })
                  }
                  error={value === null}
                  value={draft[f.key]}
                  onChange={(e) => setDraft({ ...draft, [f.key]: e.target.value })}
                  slotProps={{
                    input: {
                      inputMode: "numeric",
                      endAdornment: <InputAdornment position="end">{t(`settings.per.${f.per}`)}</InputAdornment>,
                    },
                  }}
                />
              );
            })}
          </Stack>
          <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
            <Button variant="contained" disabled={invalid || !dirty || save.isPending} onClick={() => void submit()}>
              {t("settings.save")}
            </Button>
            <Button variant="outlined" disabled={!dirty || save.isPending} onClick={() => setDraft(toDraft(data.limits))}>
              {t("settings.revert")}
            </Button>
            <Button variant="text" disabled={save.isPending} onClick={() => setDraft(toDraft(data.defaults))}>
              {t("settings.restoreDefaults")}
            </Button>
          </Box>
        </Section>
      </Grid>
      <Grid size={{ xs: 12, lg: 5 }}>
        <Section title={t("settings.notes")} description={t("settings.notesDesc")}>
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("settings.notesWindow")}
          </Typography>
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("settings.notesTrusted")}
          </Typography>
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("settings.notesRefund")}
          </Typography>
        </Section>
      </Grid>
    </Grid>
  );
}

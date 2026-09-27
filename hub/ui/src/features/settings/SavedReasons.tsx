import { useState } from "react";
import { Box, Button, IconButton, MenuItem, Select, Stack, TextField, Tooltip, Typography } from "@mui/material";
import ArrowUpwardIcon from "@mui/icons-material/ArrowUpward";
import ArrowDownwardIcon from "@mui/icons-material/ArrowDownward";
import DeleteIcon from "@mui/icons-material/DeleteOutline";
import BookmarkIcon from "@mui/icons-material/BookmarkBorder";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { ReasonPresetView, ReasonScope } from "@/models/api";
import { Section } from "@/shared/components/Section";
import { QueryView } from "@/shared/components/QueryView";
import { EmptyState } from "@/shared/components/States";
import { useCreatePreset, useDeletePreset, useReasonPresets, useUpdatePreset } from "@/features/moderation/reasons";

const scopes: ReasonScope[] = ["reject", "hide", "withdraw", "ban", "mirror_reject", "report_dismiss"];

export function SavedReasons() {
  const { t } = useTranslation();
  const presets = useReasonPresets();
  const create = useCreatePreset();
  const update = useUpdatePreset();
  const remove = useDeletePreset();
  const { notifyResult, notifyError } = useSnackbar();
  const [scope, setScope] = useState<ReasonScope>("reject");
  const [label, setLabel] = useState("");
  const [text, setText] = useState("");

  const run = async (work: () => Promise<{ notice: string; code: string }>) => {
    try {
      notifyResult(await work());
      return true;
    } catch (err) {
      notifyError(err);
      return false;
    }
  };

  const move = (list: ReasonPresetView[], index: number, delta: number) => {
    const other = list[index + delta];
    const me = list[index];
    if (!other) return;
    void run(async () => {
      await update.mutateAsync({ id: me.id, req: { scope: me.scope, label: me.label ?? "", text: me.text, position: other.position } });
      return update.mutateAsync({ id: other.id, req: { scope: other.scope, label: other.label ?? "", text: other.text, position: me.position } });
    });
  };

  return (
    <Section title={t("reasons.title")} description={t("reasons.desc")} icon={<BookmarkIcon />}>
      <QueryView query={presets}>
        {(data) => (
          <Stack spacing={2}>
            {scopes.map((s) => {
              const list = data.filter((p) => p.scope === s);
              return (
                <Box key={s}>
                  <Typography variant="metricLabel" sx={{ display: "block", mb: 0.5 }}>
                    {t(`reasons.scope.${s}`)}
                  </Typography>
                  {list.length === 0 ? (
                    <EmptyState text={t("reasons.empty")} />
                  ) : (
                    list.map((p, i) => (
                      <Box key={p.id} sx={{ display: "flex", alignItems: "center", gap: 1, py: 0.25 }}>
                        <Typography variant="body2" sx={{ flex: 1, overflowWrap: "anywhere" }}>
                          {p.label ? <strong>{p.label}: </strong> : null}
                          {p.text}
                          <Typography component="span" variant="caption" sx={{ color: colors.text.secondary, ml: 1 }}>
                            {t("reasons.uses", { count: p.uses })}
                          </Typography>
                        </Typography>
                        <IconButton size="small" disabled={i === 0} onClick={() => move(list, i, -1)}>
                          <ArrowUpwardIcon fontSize="small" />
                        </IconButton>
                        <IconButton size="small" disabled={i === list.length - 1} onClick={() => move(list, i, 1)}>
                          <ArrowDownwardIcon fontSize="small" />
                        </IconButton>
                        <Tooltip title={t("reasons.delete")}>
                          <IconButton size="small" onClick={() => void run(() => remove.mutateAsync(p.id))}>
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Box>
                    ))
                  )}
                </Box>
              );
            })}
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1} alignItems={{ sm: "flex-start" }}>
              <Select size="small" value={scope} onChange={(e) => setScope(e.target.value)} sx={{ minWidth: 180 }}>
                {scopes.map((s) => (
                  <MenuItem key={s} value={s}>
                    {t(`reasons.scope.${s}`)}
                  </MenuItem>
                ))}
              </Select>
              <TextField size="small" label={t("reasons.label")} value={label} onChange={(e) => setLabel(e.target.value)} sx={{ minWidth: 140 }} />
              <TextField size="small" label={t("reasons.text")} value={text} onChange={(e) => setText(e.target.value)} fullWidth />
              <Button
                variant="contained"
                disabled={text.trim() === "" || create.isPending}
                onClick={() =>
                  void run(() => create.mutateAsync({ scope, label, text, position: 0 })).then((ok) => {
                    if (ok) {
                      setLabel("");
                      setText("");
                    }
                  })
                }
              >
                {t("reasons.add")}
              </Button>
            </Stack>
          </Stack>
        )}
      </QueryView>
    </Section>
  );
}

import { Box, Chip, Link, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { ReasonScope } from "@/models/api";
import { useCreatePreset, useReasonPresets } from "./reasons";

interface ReasonPickerProps {
  scope: ReasonScope;
  value: string;
  onPick: (text: string) => void;
}

export function ReasonPicker({ scope, value, onPick }: Readonly<ReasonPickerProps>) {
  const { t } = useTranslation();
  const presets = useReasonPresets();
  const create = useCreatePreset();
  const { notifyResult, notifyError } = useSnackbar();
  const mine = (presets.data ?? []).filter((p) => p.scope === scope).slice(0, 9);
  const text = value.trim();
  const known = mine.some((p) => p.text === text) || (presets.data ?? []).some((p) => p.scope === scope && p.text === text);
  const save = async () => {
    try {
      notifyResult(await create.mutateAsync({ scope, label: "", text, position: 0 }));
    } catch (err) {
      notifyError(err);
    }
  };
  if (mine.length === 0 && text === "") return null;
  return (
    <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", alignItems: "center" }}>
      {mine.map((p, i) => (
        <Tooltip key={p.id} title={p.label ? p.text : ""}>
          <Chip
            size="small"
            variant={p.text === text ? "filled" : "outlined"}
            color={p.text === text ? "primary" : "default"}
            label={`${String(i + 1)}. ${p.label || p.text}`}
            onClick={() => onPick(p.text)}
            sx={{ maxWidth: 260 }}
          />
        </Tooltip>
      ))}
      {text !== "" && !known && (
        <Link component="button" type="button" variant="caption" underline="hover" disabled={create.isPending} onClick={() => void save()}>
          {t("reasons.saveAsPreset")}
        </Link>
      )}
    </Box>
  );
}

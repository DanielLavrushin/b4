import { Chip, Stack, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import type { Term } from "@/models/api";
import { flagHint, flagText, techniqueChip, techniqueText } from "@/shared/utils/terms";

export function TechniqueChips({ terms, max = 6 }: Readonly<{ terms: Term[]; max?: number }>) {
  const { t } = useTranslation();
  const shown = terms.slice(0, max);
  return (
    <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
      {shown.map((term, i) => (
        <Tooltip key={`${term.code}-${String(i)}`} title={techniqueText(t, term)}>
          <Chip size="small" variant="outlined" label={techniqueChip(t, term)} sx={{ fontSize: 11, height: 22 }} />
        </Tooltip>
      ))}
      {terms.length > max && (
        <Tooltip title={terms.slice(max).map((term) => techniqueText(t, term)).join("; ")}>
          <Chip size="small" variant="outlined" label={`+${String(terms.length - max)}`} sx={{ fontSize: 11, height: 22 }} />
        </Tooltip>
      )}
    </Stack>
  );
}

export function FlagChips({ flags }: Readonly<{ flags: string[] }>) {
  const { t } = useTranslation();
  if (flags.length === 0) return null;
  return (
    <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
      {flags.map((f) => (
        <Tooltip key={f} title={flagHint(t, f)}>
          <Chip size="small" variant="outlined" color="warning" label={flagText(t, f)} sx={{ fontSize: 11, height: 22 }} />
        </Tooltip>
      ))}
    </Stack>
  );
}

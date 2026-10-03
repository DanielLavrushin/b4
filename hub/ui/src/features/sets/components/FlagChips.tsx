import { Chip, Stack, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { flagTone } from "@/features/sets/card/badges";
import { flagHint, flagText } from "@/shared/utils/terms";

export function FlagChips({ flags }: Readonly<{ flags: string[] }>) {
  const { t } = useTranslation();
  if (flags.length === 0) return null;
  return (
    <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
      {flags.map((f) => (
        <Tooltip key={f} title={flagHint(t, f)}>
          <Chip size="small" variant="outlined" color={flagTone(f)} label={flagText(t, f)} />
        </Tooltip>
      ))}
    </Stack>
  );
}

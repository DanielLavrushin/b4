import { Box, SxProps, Theme, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { DescriptionIcon } from "@b4.icons";
import { colors } from "@design";

interface RunNoteProps {
  note?: string;
  dense?: boolean;
  sx?: SxProps<Theme>;
}

export const RunNote = ({ note, dense, sx }: RunNoteProps) => {
  const { t } = useTranslation();
  if (!note) return null;
  return (
    <Box sx={sx}>
      <Box
        aria-label={t("discovery.note.label")}
        sx={{
          display: "flex",
          alignItems: "flex-start",
          gap: 0.75,
          color: colors.text.secondary,
          minWidth: 0,
        }}
      >
        <DescriptionIcon
          sx={{ fontSize: dense ? 14 : 16, mt: "2px", flexShrink: 0 }}
        />
        <Typography
          variant={dense ? "caption" : "body2"}
          sx={{
            fontStyle: "italic",
            whiteSpace: "normal",
            overflowWrap: "anywhere",
            minWidth: 0,
          }}
        >
          {note}
        </Typography>
      </Box>
    </Box>
  );
};

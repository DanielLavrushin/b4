import { Box, Button, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { CopyIcon } from "@b4.icons";
import { colors, fonts, radiusPx, typography } from "@design";
import { copyText } from "@utils";
import { useSnackbar } from "@context/SnackbarProvider";

export interface B4ConnectDetailsProps {
  label: string;
  snippet: string;
  copyValue?: string;
  copiedMessage?: string;
  footer?: React.ReactNode;
}

export const B4ConnectDetails = ({
  label,
  snippet,
  copyValue,
  copiedMessage,
  footer,
}: B4ConnectDetailsProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();

  const copy = async () => {
    if (await copyText(copyValue ?? snippet)) {
      showSuccess(copiedMessage ?? t("core.copied"));
    } else {
      showError(t("core.copyFailed"));
    }
  };

  return (
    <Box
      sx={{
        border: `1px solid ${colors.border.light}`,
        borderRadius: `${radiusPx.md}px`,
        bgcolor: colors.background.dark,
        p: "14px 15px",
      }}
    >
      <Stack spacing={1.25}>
        <Stack
          direction="row"
          alignItems="center"
          justifyContent="space-between"
          spacing={1}
        >
          <Typography
            sx={{
              fontFamily: fonts.mono,
              fontSize: typography.sizes.xs,
              letterSpacing: typography.tracking.wide,
              textTransform: "uppercase",
              color: colors.text.secondary,
            }}
          >
            {label}
          </Typography>
          <Button
            size="small"
            variant="outlined"
            startIcon={<CopyIcon fontSize="small" />}
            onClick={() => {
              void copy();
            }}
          >
            {t("core.copy")}
          </Button>
        </Stack>

        <Box
          component="pre"
          sx={{
            m: 0,
            p: "12px 13px",
            border: `1px solid ${colors.border.light}`,
            borderRadius: `${radiusPx.sm}px`,
            bgcolor: colors.background.default,
            fontFamily: fonts.mono,
            fontSize: "0.72rem",
            lineHeight: 1.7,
            color: colors.text.secondary,
            overflowX: "auto",
          }}
        >
          {snippet}
        </Box>

        {footer}
      </Stack>
    </Box>
  );
};

export default B4ConnectDetails;

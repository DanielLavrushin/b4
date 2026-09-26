import { useMemo, useState } from "react";
import { Box, Button, Typography } from "@mui/material";
import { B4Alert, B4Dialog, B4ModalAlertStrip, B4TextField } from "@b4.elements";
import { AsnIcon, CopyIcon, InfoIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@context/SnackbarProvider";
import { AsnFacts } from "@common/AsnFacts";
import { AsnView, formatAsn } from "@models/asn";
import { copyText } from "@utils";

const PREVIEW_LIMIT = 1000;

interface AsnPrefixesDialogProps {
  view: AsnView | null;
  onClose: () => void;
}

export const AsnPrefixesDialog = ({ view, onClose }: AsnPrefixesDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [filter, setFilter] = useState("");

  const prefixes = useMemo(() => view?.prefixes ?? [], [view]);
  const matching = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    return needle
      ? prefixes.filter((p) => p.toLowerCase().includes(needle))
      : prefixes;
  }, [prefixes, filter]);
  const shown = matching.slice(0, PREVIEW_LIMIT);

  const handleCopy = async () => {
    const ok = await copyText(prefixes.join("\n"));
    if (ok) showSuccess(t("sets.targets.asn.prefixesCopied"));
    else showError(t("sets.targets.asn.prefixesCopyFailed"));
  };

  const handleClose = () => {
    setFilter("");
    onClose();
  };

  return (
    <B4Dialog
      title={view ? formatAsn(view.id) : ""}
      subtitle={view?.name || t("sets.targets.asn.unnamed")}
      icon={<AsnIcon />}
      open={!!view}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button
            startIcon={<CopyIcon />}
            disabled={prefixes.length === 0}
            onClick={() => void handleCopy()}
          >
            {t("sets.targets.asn.prefixesCopy")}
          </Button>
          <Button variant="contained" onClick={handleClose}>
            {t("core.close")}
          </Button>
        </>
      }
    >
      {view && prefixes.length === 0 && (
        <B4Alert severity="warning" noWrapper>
          {t("sets.targets.asn.prefixesEmpty")}
        </B4Alert>
      )}
      {view && prefixes.length > 0 && (
        <>
          <B4ModalAlertStrip tone="primary" icon={<InfoIcon />} sx={{ mb: 2 }}>
            <AsnFacts view={view} />
          </B4ModalAlertStrip>
          <B4TextField
            label={t("sets.targets.asn.prefixesFilter")}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="142.250."
          />
          {matching.length > PREVIEW_LIMIT && (
            <Typography
              variant="caption"
              sx={{ display: "block", mt: 1, color: colors.text.secondary }}
            >
              {t("sets.targets.asn.prefixesShowing", {
                shown: PREVIEW_LIMIT.toLocaleString(),
                total: matching.length.toLocaleString(),
              })}
            </Typography>
          )}
          <Box
            sx={{
              mt: 1.5,
              maxHeight: 420,
              overflow: "auto",
              display: "grid",
              gridTemplateColumns: {
                xs: "1fr",
                sm: "repeat(2, minmax(0, 1fr))",
              },
              gap: 0.5,
              p: 1,
              bgcolor: colors.background.dark,
              border: `1px solid ${colors.border.default}`,
              borderRadius: 1,
            }}
          >
            {shown.map((prefix) => (
              <Typography
                key={prefix}
                sx={{
                  ...typography.recipes.monoSmall,
                  color: colors.text.primary,
                  overflowWrap: "anywhere",
                }}
              >
                {prefix}
              </Typography>
            ))}
            {shown.length === 0 && (
              <Typography
                variant="body2"
                sx={{ color: colors.text.secondary }}
              >
                {t("sets.targets.asn.prefixesNoMatch")}
              </Typography>
            )}
          </Box>
        </>
      )}
    </B4Dialog>
  );
};

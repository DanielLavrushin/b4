import { Box, Button, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { B4Dialog } from "@b4.elements";
import { EditIcon, SetsIcon } from "@b4.icons";
import { colors } from "@design";
import { formatAsn } from "@models/asn";
import { HubSet } from "@models/hub";
import { CoveredSet } from "@models/sets";

export interface CoveredReport {
  set: HubSet;
  localId: string;
  name: string;
  covered: CoveredSet[];
  undoable: boolean;
  disabled: string[];
}

interface CoveredDialogProps {
  report: CoveredReport | null;
  busy: boolean;
  onDisable: (covered: CoveredSet) => void;
  onReplace: (covered: CoveredSet) => void;
  onUndo: () => void;
  onOpenSet: () => void;
  onClose: () => void;
}

const lostText = (t: TFunction, covered: CoveredSet): string => {
  const parts: string[] = [];
  if ((covered.geosite ?? []).length > 0) {
    parts.push(t("hub.card.geosite", { list: covered.geosite.join(", ") }));
  }
  if ((covered.geoip ?? []).length > 0) {
    parts.push(t("hub.card.geoip", { list: covered.geoip.join(", ") }));
  }
  if ((covered.asns ?? []).length > 0) {
    parts.push(
      t("hub.card.asns", { list: covered.asns.map(formatAsn).join(", ") }),
    );
  }
  return parts.join(" · ");
};

export const CoveredDialog = ({
  report,
  busy,
  onDisable,
  onReplace,
  onUndo,
  onOpenSet,
  onClose,
}: CoveredDialogProps) => {
  const { t } = useTranslation();
  const name = report?.name ?? "";
  const covered = report?.covered ?? [];
  const offered = covered.some(
    (c) => c.idle && !report?.disabled.includes(c.set_id),
  );

  return (
    <B4Dialog
      open={Boolean(report)}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      icon={<SetsIcon />}
      title={t("hub.apply.covered.title")}
      subtitle={name}
      actions={
        <>
          <Button onClick={onClose}>{t("core.close")}</Button>
          <Box sx={{ flex: 1 }} />
          {report?.undoable && (
            <Button disabled={busy} onClick={onUndo}>
              {t("hub.apply.undo")}
            </Button>
          )}
          <Button variant="outlined" startIcon={<EditIcon />} onClick={onOpenSet}>
            {t("hub.apply.openSet")}
          </Button>
        </>
      }
    >
      <Stack spacing={1.5}>
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("hub.apply.covered.intro", { name })}
        </Typography>
        {covered.map((c) => {
          const lost = lostText(t, c);
          const off = report?.disabled.includes(c.set_id) ?? false;
          return (
            <Stack
              key={c.set_id}
              direction={{ xs: "column", sm: "row" }}
              spacing={1.5}
              sx={{
                p: 1.5,
                alignItems: { xs: "stretch", sm: "center" },
                justifyContent: "space-between",
                border: `1px solid ${colors.border.light}`,
                borderRadius: 1.5,
                bgcolor: colors.background.dark,
              }}
            >
              <Box sx={{ minWidth: 0 }}>
                <Typography
                  variant="subtitle2"
                  sx={{ color: colors.text.primary, overflowWrap: "anywhere" }}
                >
                  {c.set_name}
                </Typography>
                {lost && (
                  <Typography
                    variant="body2"
                    sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}
                  >
                    {lost}
                  </Typography>
                )}
                <Typography
                  variant="body2"
                  sx={{
                    color:
                      c.idle && !off
                        ? colors.state.warning
                        : colors.text.disabled,
                  }}
                >
                  {off && t("hub.apply.covered.switchedOff")}
                  {!off && c.idle && t("hub.apply.covered.idle")}
                  {!off && !c.idle && t("hub.apply.covered.partial")}
                </Typography>
              </Box>
              {c.idle && !off && (
                <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}>
                  <Button
                    size="small"
                    variant="outlined"
                    disabled={busy}
                    onClick={() => onDisable(c)}
                  >
                    {t("hub.apply.covered.disable")}
                  </Button>
                  {report?.undoable && (
                    <Button
                      size="small"
                      variant="outlined"
                      disabled={busy}
                      onClick={() => onReplace(c)}
                    >
                      {t("hub.apply.replace")}
                    </Button>
                  )}
                </Stack>
              )}
            </Stack>
          );
        })}
        {offered && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {report?.undoable
              ? t("hub.apply.covered.choices")
              : t("hub.apply.covered.choiceDisable")}
          </Typography>
        )}
      </Stack>
    </B4Dialog>
  );
};

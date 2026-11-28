import { Box, Button, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";

import { CopyIcon, DownloadIcon, ImportExportIcon } from "@b4.icons";
import { B4Dialog, B4FieldMessage } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { colors, fonts, radiusPx } from "@design";
import { B4SetConfig } from "@models/config";
import { setFileName, setGroupFileName } from "@models/setGroups";
import {
  compactJson,
  copyText,
  exportSetGroupJson,
  exportSetJson,
} from "@utils";

export type ExportTarget =
  | { group: string; sets: B4SetConfig[] }
  | { set: B4SetConfig };

const previewSx = {
  m: 0,
  p: "12px 13px",
  maxHeight: 320,
  overflow: "auto",
  border: `1px solid ${colors.border.light}`,
  borderRadius: `${radiusPx.sm}px`,
  bgcolor: colors.background.default,
  fontFamily: fonts.mono,
  fontSize: "0.72rem",
  lineHeight: 1.7,
  color: colors.text.secondary,
};

interface SetExportDialogProps {
  target: ExportTarget | null;
  onClose: () => void;
}

export const SetExportDialog = ({ target, onClose }: SetExportDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();

  let title = "";
  let subtitle = "";
  let intro = "";
  let json = "";
  let fileName = "";
  let deviceFilter = false;
  if (target && "set" in target) {
    title = t("sets.export.title");
    subtitle = target.set.name;
    intro = t("sets.export.intro");
    json = exportSetJson(target.set, 2);
    fileName = setFileName(target.set.name);
    deviceFilter = (target.set.targets.source_devices ?? []).length > 0;
  } else if (target) {
    title = t("sets.groups.exportTitle");
    subtitle = target.group;
    intro = t("sets.groups.exportIntro", { count: target.sets.length });
    json = exportSetGroupJson(target.group, target.sets, 2);
    fileName = setGroupFileName(target.group);
  }

  const copy = async () => {
    const ok = await copyText(compactJson(json));
    if (ok) showSuccess(t("sets.importExport.copiedToClipboard"));
    else showError(t("sets.importExport.copyFailed"));
  };

  const download = () => {
    const url = URL.createObjectURL(
      new Blob([json], { type: "application/json" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = fileName;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <B4Dialog
      open={target !== null}
      title={title}
      subtitle={subtitle}
      icon={<ImportExportIcon />}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button startIcon={<CopyIcon />} onClick={() => void copy()}>
            {t("sets.groups.copy")}
          </Button>
          <Button
            variant="contained"
            startIcon={<DownloadIcon />}
            onClick={download}
          >
            {t("sets.groups.download")}
          </Button>
        </>
      }
    >
      <Typography sx={{ mt: 2, mb: 1.5 }}>{intro}</Typography>
      <Box component="pre" sx={previewSx}>
        {json}
      </Box>
      {deviceFilter && (
        <B4FieldMessage>{t("sets.importExport.deviceFilterWarning")}</B4FieldMessage>
      )}
    </B4Dialog>
  );
};

import { useState } from "react";
import {
  Autocomplete,
  Box,
  Button,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Switch,
  Tooltip,
  Typography,
} from "@mui/material";
import MoreVertIcon from "@mui/icons-material/MoreVert";
import { useTranslation } from "react-i18next";

import {
  CategoryIcon,
  CloseIcon,
  CopyIcon,
  DownloadIcon,
  DragIcon,
  EditIcon,
  ImportExportIcon,
  PublishIcon,
} from "@b4.icons";
import { B4Dialog, B4TextField } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { colors, fonts, radius, radiusPx } from "@design";
import { B4SetConfig } from "@models/config";
import {
  MAX_SET_GROUP_CHARS,
  normalizeSetGroup,
  setGroupFileName,
} from "@models/setGroups";
import { copyText, exportSetGroupJson } from "@utils";

export const GROUP_ROW_GAP = 44;
const TRAY_TOP = -30;
const TRAY_BOTTOM = -6;
const TRAY_END = -8;
const TRAY_JOIN = -12;
const TRAY_STACK = -GROUP_ROW_GAP / 2;

export const groupTraySx = (first: boolean, last: boolean) => ({
  "&::before": {
    content: '""',
    position: "absolute",
    zIndex: -1,
    pointerEvents: "none",
    top: { xs: first ? TRAY_TOP : TRAY_STACK, sm: TRAY_TOP },
    bottom: { xs: last ? TRAY_BOTTOM : TRAY_STACK, sm: TRAY_BOTTOM },
    left: { xs: TRAY_END, sm: first ? TRAY_END : TRAY_JOIN },
    right: { xs: TRAY_END, sm: last ? TRAY_END : TRAY_JOIN },
    bgcolor: colors.background.dark,
    borderStyle: "solid",
    borderColor: colors.border.default,
    borderLeftColor: { xs: colors.secondary, sm: first ? colors.secondary : colors.border.default },
    borderTopWidth: { xs: first ? 1 : 0, sm: 1 },
    borderBottomWidth: { xs: last ? 1 : 0, sm: 1 },
    borderLeftWidth: { xs: 3, sm: first ? 3 : 0 },
    borderRightWidth: { xs: 1, sm: last ? 1 : 0 },
    borderTopLeftRadius: first ? radius.md : 0,
    borderTopRightRadius: { xs: first ? radius.md : 0, sm: last ? radius.md : 0 },
    borderBottomLeftRadius: { xs: last ? radius.md : 0, sm: first ? radius.md : 0 },
    borderBottomRightRadius: last ? radius.md : 0,
  },
});

interface SetGroupHeaderProps {
  group: string;
  count: number;
  enabledCount: number;
  selectionMode: boolean;
  handleProps?: React.HTMLAttributes<HTMLDivElement>;
  onToggleEnabled: (enabled: boolean) => void;
  onRename: () => void;
  onExport: () => void;
  onPublish?: () => void;
  onUngroup: () => void;
}

export const SetGroupHeader = ({
  group,
  count,
  enabledCount,
  selectionMode,
  handleProps,
  onToggleEnabled,
  onRename,
  onExport,
  onPublish,
  onUngroup,
}: SetGroupHeaderProps) => {
  const { t } = useTranslation();
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const anyEnabled = enabledCount > 0;
  const partly = anyEnabled && enabledCount < count;

  const run = (action: () => void) => {
    setMenuAnchor(null);
    action();
  };

  return (
    <Box
      sx={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: "100%",
        height: 24,
        mb: "3px",
        display: "flex",
        alignItems: "center",
        gap: 0.75,
        minWidth: 0,
      }}
    >
      {!selectionMode && (
        <Box
          {...handleProps}
          data-group-handle
          aria-label={t("sets.groups.dragGroup")}
          sx={{
            cursor: "grab",
            touchAction: "none",
            color: colors.text.secondary,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            minWidth: 24,
            height: 24,
            ml: "-2px",
            "&:hover": { color: colors.secondary },
          }}
        >
          <DragIcon sx={{ fontSize: 18 }} />
        </Box>
      )}
      <CategoryIcon sx={{ fontSize: 16, color: colors.secondary }} />
      <Typography
        variant="body2"
        fontWeight={600}
        noWrap
        title={group}
        sx={{ minWidth: 0 }}
      >
        {group}
      </Typography>
      <Typography
        variant="caption"
        color="text.secondary"
        noWrap
        sx={{ flexShrink: 0 }}
      >
        {partly
          ? t("sets.groups.partlyEnabled", { enabled: enabledCount, count })
          : t("sets.groups.setCount", { count })}
      </Typography>
      {!selectionMode && (
        <>
          <Tooltip
            title={
              anyEnabled
                ? t("sets.groups.disableAll")
                : t("sets.groups.enableAll")
            }
          >
            <Switch
              size="small"
              checked={anyEnabled}
              onChange={(_, checked) => onToggleEnabled(checked)}
              sx={partly ? { "& .MuiSwitch-track": { opacity: 0.25 } } : null}
            />
          </Tooltip>
          <Tooltip title={t("sets.groups.actions")}>
            <IconButton
              size="small"
              aria-label={t("sets.groups.actions")}
              onClick={(e) => setMenuAnchor(e.currentTarget)}
              sx={{ p: "2px" }}
            >
              <MoreVertIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Menu
            anchorEl={menuAnchor}
            open={menuAnchor !== null}
            onClose={() => setMenuAnchor(null)}
            transformOrigin={{ horizontal: "right", vertical: "top" }}
            anchorOrigin={{ horizontal: "right", vertical: "bottom" }}
          >
            <MenuItem onClick={() => run(onRename)}>
              <ListItemIcon>
                <EditIcon fontSize="small" />
              </ListItemIcon>
              <ListItemText>{t("sets.groups.rename")}</ListItemText>
            </MenuItem>
            <MenuItem onClick={() => run(onExport)}>
              <ListItemIcon>
                <DownloadIcon fontSize="small" />
              </ListItemIcon>
              <ListItemText>{t("sets.groups.export")}</ListItemText>
            </MenuItem>
            {onPublish && (
              <MenuItem onClick={() => run(onPublish)}>
                <ListItemIcon>
                  <PublishIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText>{t("sets.groups.publish.menu")}</ListItemText>
              </MenuItem>
            )}
            <MenuItem onClick={() => run(onUngroup)}>
              <ListItemIcon>
                <CloseIcon fontSize="small" />
              </ListItemIcon>
              <ListItemText>{t("sets.groups.ungroup")}</ListItemText>
            </MenuItem>
          </Menu>
        </>
      )}
    </Box>
  );
};

interface SetGroupNameDialogProps {
  title: string;
  subtitle?: string;
  initial: string;
  groups: string[];
  confirmLabel: string;
  onConfirm: (group: string) => void;
  onRemove?: () => void;
  onClose: () => void;
}

export const SetGroupNameDialog = ({
  title,
  subtitle,
  initial,
  groups,
  confirmLabel,
  onConfirm,
  onRemove,
  onClose,
}: SetGroupNameDialogProps) => {
  const { t } = useTranslation();
  const [value, setValue] = useState(initial);
  const name = normalizeSetGroup(value);

  return (
    <B4Dialog
      open
      title={title}
      subtitle={subtitle}
      icon={<CategoryIcon />}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Box sx={{ flex: 1 }} />
          {onRemove && (
            <Button onClick={onRemove}>{t("sets.groups.removeFromGroup")}</Button>
          )}
          <Button
            variant="contained"
            disabled={name === ""}
            onClick={() => onConfirm(name)}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <Autocomplete
        freeSolo
        options={groups}
        inputValue={value}
        onInputChange={(_, next) => setValue(next)}
        onChange={(event, next) => {
          if (typeof next !== "string") return;
          setValue(next);
          const picked = normalizeSetGroup(next);
          if (event.type === "keydown" && picked !== "") onConfirm(picked);
        }}
        renderInput={(params) => (
          <B4TextField
            {...params}
            autoFocus
            slotProps={{
              htmlInput: {
                ...params.inputProps,
                maxLength: MAX_SET_GROUP_CHARS,
              },
            }}
            label={t("sets.groups.name")}
            helperText={t("sets.groups.nameHelp")}
          />
        )}
        sx={{ mt: 2 }}
      />
    </B4Dialog>
  );
};

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

interface SetGroupExportDialogProps {
  group: string | null;
  sets: B4SetConfig[];
  onClose: () => void;
}

export const SetGroupExportDialog = ({
  group,
  sets,
  onClose,
}: SetGroupExportDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const json = group === null ? "" : exportSetGroupJson(group, sets, 2);

  const copy = async () => {
    const ok = await copyText(json);
    if (ok) showSuccess(t("sets.importExport.copiedToClipboard"));
    else showError(t("sets.importExport.copyFailed"));
  };

  const download = () => {
    const url = URL.createObjectURL(
      new Blob([json], { type: "application/json" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = setGroupFileName(group ?? "");
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <B4Dialog
      open={group !== null}
      title={t("sets.groups.exportTitle")}
      subtitle={group ?? undefined}
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
      <Typography sx={{ mt: 2, mb: 1.5 }}>
        {t("sets.groups.exportIntro", { count: sets.length })}
      </Typography>
      <Box component="pre" sx={previewSx}>
        {json}
      </Box>
    </B4Dialog>
  );
};

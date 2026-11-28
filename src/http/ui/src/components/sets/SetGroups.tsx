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
  DownloadIcon,
  DragIcon,
  EditIcon,
  ShareIcon,
} from "@b4.icons";
import { B4Dialog, B4TextField } from "@b4.elements";
import { colors } from "@design";
import { MAX_SET_GROUP_CHARS, normalizeSetGroup } from "@models/setGroups";

interface SetGroupHeaderProps {
  group: string;
  count: number;
  enabledCount: number;
  selectionMode: boolean;
  handleProps?: React.HTMLAttributes<HTMLDivElement>;
  onToggleEnabled: (enabled: boolean) => void;
  onRename: () => void;
  onExport: () => void;
  onShare?: () => void;
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
  onShare,
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
          {onShare && (
            <Tooltip title={t("sets.share.sectionTitle")}>
              <IconButton
                size="small"
                aria-label={t("sets.share.sectionTitle")}
                onClick={onShare}
                sx={{ p: "2px" }}
              >
                <ShareIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
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
            {onShare && (
              <MenuItem onClick={() => run(onShare)}>
                <ListItemIcon>
                  <ShareIcon fontSize="small" />
                </ListItemIcon>
                <ListItemText>{t("sets.share.menu")}</ListItemText>
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


import { useState } from "react";
import { IconButton, Tooltip, Menu, MenuItem } from "@mui/material";
import { AddCircleOutline as AddIcon } from "@mui/icons-material";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { setsApi } from "@b4.sets";
import { useTranslation } from "react-i18next";

interface AddToSetButtonProps {
  domain: string;
  sets: B4SetConfig[];
  onAdded: () => void;
}

export const AddToSetButton = ({ domain, sets, onAdded }: AddToSetButtonProps) => {
  const { t } = useTranslation();
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null);
  const [adding, setAdding] = useState(false);
  const enabledSets = sets.filter((s) => s.enabled);

  if (enabledSets.length === 0) return null;

  const handleAdd = async (setId: string) => {
    setAnchorEl(null);
    setAdding(true);
    try {
      await setsApi.addDomainToSet(setId, domain);
      onAdded();
    } catch (e) {
      console.error("Failed to add domain:", e);
    } finally {
      setAdding(false);
    }
  };

  return (
    <>
      <Tooltip title={t("core.addToSet")}>
        <IconButton
          size="small"
          onClick={(e) => setAnchorEl(e.currentTarget)}
          disabled={adding}
          sx={{
            color: colors.text.secondary,
            p: 0.25,
            "&:hover": { color: colors.secondary },
          }}
        >
          <AddIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Tooltip>
      <Menu
        anchorEl={anchorEl}
        open={Boolean(anchorEl)}
        onClose={() => setAnchorEl(null)}
        slotProps={{
          paper: {
            sx: {
              bgcolor: colors.background.default,
              border: `1px solid ${colors.border.default}`,
            },
          },
        }}
      >
        {enabledSets.map((set) => (
          <MenuItem
            key={set.id}
            onClick={() => void handleAdd(set.id)}
            sx={{ color: colors.text.primary, fontSize: "0.8rem" }}
          >
            {set.name}
          </MenuItem>
        ))}
      </Menu>
    </>
  );
};

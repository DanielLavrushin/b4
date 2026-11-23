import { Menu, MenuItem } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Badge } from "@b4.elements";
import type { B4SetConfig } from "@models/config";

interface SetMenuProps {
  anchor: HTMLElement | null;
  sets: B4SetConfig[];
  onPick: (setId: string) => void;
  onClose: () => void;
}

export const SetMenu = ({ anchor, sets, onPick, onClose }: SetMenuProps) => {
  const { t } = useTranslation();
  return (
    <Menu open={!!anchor} anchorEl={anchor} onClose={onClose}>
      {sets.map((set) => (
        <MenuItem
          key={set.id}
          onClick={() => {
            onPick(set.id);
            onClose();
          }}
        >
          {set.name}
          {!set.enabled && <B4Badge label={t("core.disabled")} size="small" sx={{ ml: 1 }} />}
        </MenuItem>
      ))}
    </Menu>
  );
};

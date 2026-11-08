import { Button, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { NotifyIcon, NotifyOffIcon, NotifyOnIcon } from "@b4.icons";
import { setPopups, usePopupState } from "@/stores/notify";

interface B4NotifyToggleProps {
  hint: string;
}

export const B4NotifyToggle = ({ hint }: B4NotifyToggleProps) => {
  const { t } = useTranslation();
  const state = usePopupState();
  if (state === "unavailable") return null;

  const on = state === "on";
  const blocked = state === "blocked";
  const icon = on ? <NotifyOnIcon /> : blocked ? <NotifyOffIcon /> : <NotifyIcon />;
  const tip = on
    ? t("notify.hintOn")
    : blocked
      ? t("notify.hintBlocked")
      : hint;

  return (
    <Tooltip title={tip}>
      <span>
        <Button
          size="small"
          startIcon={icon}
          disabled={blocked}
          aria-pressed={on}
          onClick={() => void setPopups(!on)}
          sx={{
            color: on ? colors.secondary : colors.text.secondary,
            textTransform: "none",
            whiteSpace: "nowrap",
          }}
        >
          {on ? t("notify.on") : blocked ? t("notify.blocked") : t("notify.off")}
        </Button>
      </span>
    </Tooltip>
  );
};

export default B4NotifyToggle;

import { IconButton } from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon } from "@b4.icons";
import { colors } from "@design";

interface B4PlusButtonProps {
  onClick: () => void;
  disabled?: boolean;
}

export const B4PlusButton = ({
  onClick,
  disabled = false,
}: B4PlusButtonProps) => {
  const { t } = useTranslation();
  return (
    <IconButton
      onClick={onClick}
      disabled={disabled}
      aria-label={t("core.add")}
      sx={{
        bgcolor: colors.accent.secondary,
        color: colors.secondary,
        "&:hover": {
          bgcolor: colors.accent.secondaryHover,
        },
        "&:disabled": {
          bgcolor: colors.accent.secondaryHover,
          color: colors.accent.secondary,
        },
      }}
    >
      <AddIcon />
    </IconButton>
  );
};

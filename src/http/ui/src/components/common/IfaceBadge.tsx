import { useTranslation } from "react-i18next";
import { Box } from "@mui/material";
import { B4Alert } from "@common/B4Alert";
import { B4Badge } from "@common/B4Badge";
import { useChangedArrayItem } from "@context/ChangedFieldsContext";
import { colors } from "@design";

const changedSx = {
  borderColor: colors.secondary,
  "&.MuiChip-outlinedPrimary": {
    borderColor: colors.secondary,
  },
  "&.MuiChip-filledPrimary": {
    backgroundColor: colors.accent.secondary,
    color: colors.secondary,
  },
};

export const IfaceBadge = ({
  path,
  iface,
  label,
  variant,
  color,
  onClick,
}: {
  path: string;
  iface: string;
  label: string;
  variant: "filled" | "outlined";
  color: "error" | "primary";
  onClick?: () => void;
}) => {
  const { t } = useTranslation();
  const changed = useChangedArrayItem(
    path,
    iface,
    t("core.interfaceItem", { name: iface }),
  );
  return (
    <B4Badge
      label={label}
      onClick={onClick}
      variant={variant}
      color={color}
      sx={changed ? changedSx : undefined}
    />
  );
};

export interface InterfaceChipsProps {
  available: string[];
  selected: string[];
  disabled: boolean;
  onToggle: (iface: string) => void;
  path: string;
  showMissing?: boolean;
}

export const InterfaceChips = ({
  available,
  selected,
  disabled,
  onToggle,
  path,
  showMissing = true,
}: InterfaceChipsProps) => {
  const { t } = useTranslation();
  const toggle = (iface: string) =>
    disabled ? undefined : () => onToggle(iface);

  return (
    <>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
        {showMissing &&
          selected
            .filter((iface) => !available.includes(iface))
            .map((iface) => (
              <IfaceBadge
                key={iface}
                path={path}
                iface={iface}
                label={`${iface} (${t("settings.Feature.missingIface")})`}
                onClick={toggle(iface)}
                variant="filled"
                color="error"
              />
            ))}
        {available.map((iface) => (
          <IfaceBadge
            key={iface}
            path={path}
            iface={iface}
            label={iface}
            onClick={toggle(iface)}
            variant={selected.includes(iface) ? "filled" : "outlined"}
            color="primary"
          />
        ))}
      </Box>
      {available.length === 0 && (
        <B4Alert severity="warning" sx={{ mt: 1 }}>
          {t("settings.Feature.noInterfacesDetected")}
        </B4Alert>
      )}
    </>
  );
};

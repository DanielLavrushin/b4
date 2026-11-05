import { useTranslation } from "react-i18next";
import { Box } from "@mui/material";
import { B4Alert } from "@common/B4Alert";
import { B4Badge } from "@common/B4Badge";
import { useChangedArrayItem } from "@context/ChangedFieldsContext";
import { colors } from "@design";
import { IfaceDetail } from "@models/config";

export type IfaceDetails = Record<string, IfaceDetail>;

export const useIfaceLabel = (details?: IfaceDetails) => {
  const { t } = useTranslation();
  return (iface: string): string => {
    const detail = details?.[iface];
    const name = detail?.label ? `${detail.label} (${iface})` : iface;
    return detail?.up === false
      ? `${name} · ${t("settings.Feature.ifaceDown")}`
      : name;
  };
};

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
  down,
}: {
  path: string;
  iface: string;
  label: string;
  variant: "filled" | "outlined";
  color: "error" | "primary";
  onClick?: () => void;
  down?: boolean;
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
      sx={{ ...(changed ? changedSx : {}), ...(down ? { opacity: 0.6 } : {}) }}
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
  details?: IfaceDetails;
}

export const InterfaceChips = ({
  available,
  selected,
  disabled,
  onToggle,
  path,
  showMissing = true,
  details,
}: InterfaceChipsProps) => {
  const { t } = useTranslation();
  const ifaceLabel = useIfaceLabel(details);
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
            label={ifaceLabel(iface)}
            down={details?.[iface]?.up === false}
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

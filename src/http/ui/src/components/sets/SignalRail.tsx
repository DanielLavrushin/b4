import { useTranslation } from "react-i18next";
import {
  FacetCompareBar as SharedFacetCompareBar,
  FacetDrawer as SharedFacetDrawer,
  SignalRail as SharedSignalRail,
  type FacetCompareBarProps,
  type FacetDrawerProps,
  type SignalRailProps,
} from "@design";

export type { FacetToggleMode } from "@design";

export const SignalRail = (props: Omit<SignalRailProps, "t">) => {
  const { t } = useTranslation();
  return <SharedSignalRail {...props} t={t} />;
};

export const FacetDrawer = (
  props: Omit<FacetDrawerProps, "t" | "onEdit"> & { onEdit: () => void },
) => {
  const { t } = useTranslation();
  return <SharedFacetDrawer {...props} t={t} />;
};

export const FacetCompareBar = (props: Omit<FacetCompareBarProps, "t">) => {
  const { t } = useTranslation();
  return <SharedFacetCompareBar {...props} t={t} />;
};

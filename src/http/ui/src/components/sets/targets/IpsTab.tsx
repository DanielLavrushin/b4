import { useState } from "react";
import { Grid } from "@mui/material";
import { IpIcon } from "@b4.icons";
import { B4Hint } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { useTranslation } from "react-i18next";
import { SetStats } from "../Manager";
import { ManualEntryPanel } from "./ManualEntryPanel";
import { GeoCategoryPanel } from "./GeoCategoryPanel";
import { OtherSetsTargets, findSetOverlaps } from "./overlap";

interface IpsTabProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  otherSetsTargets?: OtherSetsTargets;
  geoipCategories: string[];
  geoipLoading: boolean;
  onChange: (field: string, value: string | string[] | boolean) => void;
}

export const IpsTab = ({
  config,
  geo,
  stats,
  otherSetsTargets,
  geoipCategories,
  geoipLoading,
  onChange,
}: IpsTabProps) => {
  const { t } = useTranslation();
  const [duplicateWarning, setDuplicateWarning] = useState("");

  const checkDuplicates = (input: string) => {
    if (!input.trim()) {
      setDuplicateWarning("");
      return;
    }
    setDuplicateWarning(findSetOverlaps(input, otherSetsTargets));
  };

  return (
    <>
      <B4Hint>{t("sets.targets.ipAlert")}</B4Hint>

      <Grid container spacing={2}>
        <Grid size={{ sm: 12, md: 6 }}>
          <ManualEntryPanel
            icon={<IpIcon />}
            title={t("sets.targets.manualIps")}
            tooltip={t("sets.targets.manualIpsTooltip")}
            inputLabel={t("sets.targets.addIpLabel")}
            inputHelper={t("sets.targets.addIpHelper")}
            inputPlaceholder={t("sets.targets.addIpPlaceholder")}
            activeTitle={t("sets.targets.activeIps")}
            emptyMessage={t("sets.targets.noIpsAdded")}
            items={config.targets.ip}
            warning={duplicateWarning}
            onItemsChange={(items) => onChange("targets.ip", items)}
            onInputChange={checkDuplicates}
          />
        </Grid>

        {geo.ipdat_path && geoipCategories.length > 0 && (
          <Grid size={{ sm: 12, md: 6 }}>
            <GeoCategoryPanel
              title={t("sets.targets.geoipCategories")}
              tooltip={t("sets.targets.geoipCatTooltip")}
              inputLabel={t("sets.targets.addGeoipLabel")}
              inputPlaceholder={t("sets.targets.addGeoipPlaceholder")}
              helperText={t("sets.targets.geoipCatAvailable", {
                count: geoipCategories.length,
              })}
              activeTitle={t("sets.targets.activeGeoipCategories")}
              options={geoipCategories}
              loading={geoipLoading}
              selected={config.targets.geoip_categories}
              breakdown={stats?.geoip_category_breakdown}
              onSelectedChange={(categories) =>
                onChange("targets.geoip_categories", categories)
              }
            />
          </Grid>
        )}
      </Grid>
    </>
  );
};

import { useMemo, useState } from "react";
import { Box, Grid } from "@mui/material";
import { IpIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4Switch } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { useTranslation } from "react-i18next";
import { SetStats } from "../Manager";
import { ManualEntryPanel } from "./ManualEntryPanel";
import { GeoCategoryPanel } from "./GeoCategoryPanel";
import { OtherSetsTargets, findSetOverlaps } from "./overlap";
import { useChangedFields } from "@context/ChangedFieldsContext";
import {
  IPV4_CATCH_ALL,
  IPV6_CATCH_ALL,
  ipCatchAllEntries,
  ipCatchAllVersion,
  ipEntryNotice,
  isIpCatchAll,
  normalizeIpEntry,
  setIpCatchAll,
} from "./catchall";

interface IpsTabProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  otherSetsTargets?: OtherSetsTargets;
  geoipCategories: string[];
  geoipLoading: boolean;
  ipv6?: boolean;
  onChange: (field: string, value: string | string[] | boolean) => void;
}

const catchAllEntryFor = (entry: string): string =>
  ipCatchAllVersion(entry) === 6 ? IPV6_CATCH_ALL : IPV4_CATCH_ALL;

export const IpsTab = ({
  config,
  geo,
  stats,
  otherSetsTargets,
  geoipCategories,
  geoipLoading,
  ipv6,
  onChange,
}: IpsTabProps) => {
  const { t } = useTranslation();
  const { changedValues } = useChangedFields();
  const [duplicateWarning, setDuplicateWarning] = useState("");
  const [entryNotice, setEntryNotice] = useState("");
  const ips = config.targets.ip ?? [];
  const catchAllEntries = ipCatchAllEntries(!!ipv6);
  const presentCatchAll = ips.filter(isIpCatchAll).map(catchAllEntryFor);
  const ipsLeaf = changedValues.get("targets.ip");
  const ipsLeafSets = useMemo(() => {
    if (
      !ipsLeaf ||
      !Array.isArray(ipsLeaf.after) ||
      !Array.isArray(ipsLeaf.before)
    ) {
      return null;
    }
    return {
      after: new Set(ipsLeaf.after as unknown[]),
      before: new Set(ipsLeaf.before as unknown[]),
    };
  }, [ipsLeaf]);
  const isIpChanged = (ip: string): boolean =>
    !!ipsLeafSets && ipsLeafSets.after.has(ip) !== ipsLeafSets.before.has(ip);
  const missingCatchAll = catchAllEntries.filter(
    (entry) => !presentCatchAll.includes(entry),
  );
  const catchAll = missingCatchAll.length === 0;
  const partialCatchAll = presentCatchAll.length > 0 && !catchAll;

  const checkDuplicates = (input: string) => {
    const notice = ipEntryNotice(input, !!ipv6);
    setEntryNotice(
      notice
        ? t("sets.targets.catchAllIpNotice", {
            value: notice.values.join(", "),
          })
        : "",
    );

    if (!input.trim()) {
      setDuplicateWarning("");
      return;
    }
    setDuplicateWarning(findSetOverlaps(input, otherSetsTargets));
  };

  const chipLabel = (item: string) => {
    const version = ipCatchAllVersion(item);
    if (version === 4) return t("sets.targets.catchAllChipV4");
    if (version === 6) return t("sets.targets.catchAllChipV6");
    return item;
  };

  return (
    <>
      <B4Hint>{t("sets.targets.ipAlert")}</B4Hint>

      <Box sx={{ my: 3, maxWidth: 360 }}>
        <B4Switch
          label={t("sets.targets.catchAllIps")}
          description={t("sets.targets.catchAllIpsDesc", {
            value: catchAllEntries.join(", "),
          })}
          checked={catchAll}
          path="targets.ip"
          arrayItem={IPV4_CATCH_ALL}
          onChange={(checked: boolean) =>
            onChange("targets.ip", setIpCatchAll(ips, checked, !!ipv6))
          }
        />
      </Box>

      {partialCatchAll && (
        <Grid container sx={{ mb: 2 }}>
          <B4Alert severity="info">
            {t("sets.targets.catchAllIpsPartial", {
              present: presentCatchAll.join(", "),
              missing: missingCatchAll.join(", "),
            })}
          </B4Alert>
        </Grid>
      )}

      {(catchAll || partialCatchAll) && (
        <Grid container sx={{ mb: 2 }}>
          <B4Alert severity="warning">
            {t("sets.targets.catchAllIpsActive")}
          </B4Alert>
        </Grid>
      )}

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
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
            notice={entryNotice}
            normalize={(raw) => normalizeIpEntry(raw, !!ipv6)}
            highlight={(item) => isIpCatchAll(item) || isIpChanged(item)}
            getItemLabel={chipLabel}
            onItemsChange={(items) => onChange("targets.ip", items)}
            onInputChange={checkDuplicates}
          />
        </Grid>

        {geo.ipdat_path && geoipCategories.length > 0 && (
          <Grid size={{ xs: 12, md: 6 }}>
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
              path="targets.geoip_categories"
            />
          </Grid>
        )}
      </Grid>
    </>
  );
};

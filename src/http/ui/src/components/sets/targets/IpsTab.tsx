import { useMemo, useState } from "react";
import { Box, Grid } from "@mui/material";
import { IpIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4Switch } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { parseAsnInput } from "@models/asn";
import { useTranslation } from "react-i18next";
import { SetStats } from "../Manager";
import { ManualEntryPanel } from "./ManualEntryPanel";
import { GeoCategoryPanel } from "./GeoCategoryPanel";
import { AsnPanel } from "./AsnPanel";
import { OtherSetsTargets, findSetOverlaps, geoipOverlapKey } from "./overlap";
import { CategoryPreviewDialog } from "./CategoryPreviewDialog";
import { useChangedFields } from "@context/ChangedFieldsContext";
import {
  IPV4_CATCH_ALL,
  IPV6_CATCH_ALL,
  ipCatchAllEntries,
  ipCatchAllVersion,
  ipEntryNotice,
  isIpCatchAll,
  isIpTarget,
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
  scrollToAsn?: boolean;
  onChange: (
    field: string,
    value: string | string[] | boolean | Record<string, string[]>,
  ) => void;
}

const TWO_COLUMNS = "@container (min-width: 760px)";
const THREE_COLUMNS = "@container (min-width: 1200px)";

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
  scrollToAsn,
  onChange,
}: IpsTabProps) => {
  const { t } = useTranslation();
  const { changedValues } = useChangedFields();
  const [duplicateWarning, setDuplicateWarning] = useState("");
  const [entryNotice, setEntryNotice] = useState("");
  const ips = config.targets.ip ?? [];
  const geoipExclude = config.targets.geoip_exclude ?? {};
  const [previewCategory, setPreviewCategory] = useState<string | null>(null);
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
  const tlsFiltered = !!config.targets.tls;
  const hasGeoip = !!geo.ipdat_path && geoipCategories.length > 0;

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

  const ipEntryProblem = (entry: string): string | null => {
    if (isIpTarget(entry)) return null;
    const { kind } = parseAsnInput(entry);
    return kind === "asn" || kind === "reserved"
      ? t("sets.targets.ipIsAsn", { value: entry })
      : t("sets.targets.ipInvalid", { value: entry });
  };

  const chipLabel = (item: string) => {
    const version = ipCatchAllVersion(item);
    if (version === 4) return t("sets.targets.catchAllChipV4");
    if (version === 6) return t("sets.targets.catchAllChipV6");
    return item;
  };

  return (
    <>
      <B4Hint>
        {t("sets.targets.ipAlert")} {t("sets.targets.ipOverlap")}
      </B4Hint>

      <Box sx={{ my: 3, display: "flex", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ maxWidth: 360, flex: "1 1 260px", minWidth: 0 }}>
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
        {!tlsFiltered && (
          <Box sx={{ maxWidth: 360, flex: "1 1 260px", minWidth: 0 }}>
            <B4Switch
              description={t("sets.targets.ipPriorityDesc")}
              checked={config.targets.ip_priority ?? false}
              path="targets.ip_priority"
              onChange={(checked: boolean) =>
                onChange("targets.ip_priority", checked)
              }
              aiTopic="targets.ip_priority"
            />
          </Box>
        )}
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

      <Box sx={{ containerType: "inline-size" }}>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: "minmax(0, 1fr)",
            columnGap: 3,
            rowGap: 4,
            [TWO_COLUMNS]: { gridTemplateColumns: "repeat(2, minmax(0, 1fr))" },
            ...(hasGeoip && {
              [THREE_COLUMNS]: {
                gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
              },
            }),
          }}
        >
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
            validate={ipEntryProblem}
            highlight={(item) => isIpCatchAll(item) || isIpChanged(item)}
            getItemLabel={chipLabel}
            onItemsChange={(items) => onChange("targets.ip", items)}
            onInputChange={checkDuplicates}
          />

          {hasGeoip && (
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
              onPreview={setPreviewCategory}
              excluded={geoipExclude}
              otherSets={(c) => otherSetsTargets?.get(geoipOverlapKey(c)) ?? []}
              path="targets.geoip_categories"
            />
          )}

          <Box
            sx={
              hasGeoip
                ? {
                    [TWO_COLUMNS]: { gridColumn: "1 / -1" },
                    [THREE_COLUMNS]: { gridColumn: "auto" },
                  }
                : undefined
            }
          >
            <AsnPanel
              config={config}
              stats={stats}
              otherSetsTargets={otherSetsTargets}
              scrollIntoView={scrollToAsn && !geoipLoading}
              onChange={onChange}
            />
          </Box>
        </Box>
      </Box>

      <CategoryPreviewDialog
        kind="geoip"
        category={previewCategory}
        excluded={previewCategory ? geoipExclude[previewCategory] : undefined}
        onExcludedChange={(entries) => {
          if (!previewCategory) return;
          onChange("targets.geoip_exclude", {
            ...geoipExclude,
            [previewCategory]: entries,
          });
        }}
        onClose={() => setPreviewCategory(null)}
      />
    </>
  );
};

import { useState, useRef, useCallback, useEffect } from "react";
import { Box, Grid } from "@mui/material";
import { DomainIcon } from "@b4.icons";
import { B4Hint, B4Select, B4Switch } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { useTranslation } from "react-i18next";
import { SetStats } from "../Manager";
import { ManualEntryPanel } from "./ManualEntryPanel";
import { GeoCategoryPanel } from "./GeoCategoryPanel";
import { CategoryPreviewDialog } from "./CategoryPreviewDialog";

interface SetDomainMatch {
  domain: string;
  set_name: string;
  set_id: string;
  via: string;
  relation: string;
  entry: string;
  enabled: boolean;
}

const RELATION_KEYS: Record<string, string> = {
  exact: "sets.targets.overlapExact",
  covered: "sets.targets.overlapCovered",
  regexp: "sets.targets.overlapRegexp",
  covers: "sets.targets.overlapCovers",
};

interface DomainsTabProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  ipv4?: boolean;
  ipv6?: boolean;
  geositeCategories: string[];
  geositeLoading: boolean;
  onChange: (field: string, value: string | string[] | boolean) => void;
}

export const DomainsTab = ({
  config,
  geo,
  stats,
  ipv4,
  ipv6,
  geositeCategories,
  geositeLoading,
  onChange,
}: DomainsTabProps) => {
  const { t } = useTranslation();
  const showIpVersionFilter = (!!ipv4 && !!ipv6) || !!config.targets.ip_version;
  const [duplicateWarning, setDuplicateWarning] = useState("");
  const [previewCategory, setPreviewCategory] = useState<string | null>(null);
  const checkTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const checkSeq = useRef(0);

  const describeMatch = useCallback(
    (match: SetDomainMatch) => {
      const via = t(
        match.via === "geosite"
          ? "sets.targets.overlapViaGeosite"
          : "sets.targets.overlapViaManual",
      );
      const source = match.enabled
        ? via
        : `${via}, ${t("sets.targets.overlapDisabled")}`;
      return t(RELATION_KEYS[match.relation] ?? RELATION_KEYS.exact, {
        domain: match.domain,
        entry: match.entry,
        set: match.set_name,
        via: source,
      });
    },
    [t],
  );

  const checkDomainBackend = useCallback(
    (input: string) => {
      const request = ++checkSeq.current;
      fetch(
        `/api/sets/check-domain?domain=${encodeURIComponent(input)}&exclude=${encodeURIComponent(config.id)}`,
      )
        .then((res) => res.json())
        .then((matches: SetDomainMatch[]) => {
          if (request !== checkSeq.current) return;
          setDuplicateWarning(
            Array.isArray(matches) ? matches.map(describeMatch).join("; ") : "",
          );
        })
        .catch(() => {});
    },
    [config.id, describeMatch],
  );

  const checkDuplicates = (input: string) => {
    clearTimeout(checkTimer.current);
    if (!input.trim()) {
      checkSeq.current++;
      setDuplicateWarning("");
      return;
    }
    checkTimer.current = setTimeout(() => checkDomainBackend(input), 400);
  };

  useEffect(() => () => clearTimeout(checkTimer.current), []);

  return (
    <>
      <B4Hint>{t("sets.targets.domainAlert")}</B4Hint>

      <Box sx={{ my: 3, display: "flex", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ maxWidth: 360, flex: 1, minWidth: 260 }}>
          <B4Switch
            label={t("sets.targets.domainOnly")}
            description={t("sets.targets.domainOnlyDesc")}
            checked={config.targets.domain_only ?? false}
            onChange={(checked: boolean) =>
              onChange("targets.domain_only", checked)
            }
            aiTopic="targets.domain_only"
          />
        </Box>
        <Box sx={{ maxWidth: 260, flex: 1, minWidth: 200 }}>
          <B4Select
            label={t("sets.targets.tlsVersionFilter")}
            value={config.targets.tls ?? ""}
            options={[
              { value: "", label: t("sets.targets.tlsAny") },
              { value: "1.2", label: "TLS 1.2" },
              { value: "1.3", label: "TLS 1.3" },
            ]}
            helperText={t("sets.targets.tlsHelperText")}
            onChange={(e) => onChange("targets.tls", e.target.value as string)}
          />
        </Box>
        {showIpVersionFilter && (
          <Box sx={{ maxWidth: 260, flex: 1, minWidth: 200 }}>
            <B4Select
              label={t("sets.targets.ipVersionFilter")}
              value={config.targets.ip_version ?? ""}
              options={[
                { value: "", label: t("sets.targets.ipVersionAny") },
                { value: "4", label: "IPv4" },
                { value: "6", label: "IPv6" },
              ]}
              helperText={t("sets.targets.ipVersionHelperText")}
              onChange={(e) =>
                onChange("targets.ip_version", e.target.value as string)
              }
            />
          </Box>
        )}
      </Box>

      <Grid container spacing={2}>
        <Grid size={{ sm: 12, md: 6 }}>
          <ManualEntryPanel
            icon={<DomainIcon />}
            title={t("sets.targets.manualDomains")}
            tooltip={t("sets.targets.manualDomainsTooltip")}
            inputLabel={t("sets.targets.addDomainLabel")}
            inputHelper={t("sets.targets.addDomainHelper")}
            inputPlaceholder={t("sets.targets.addDomainPlaceholder")}
            activeTitle={t("sets.targets.activeDomains")}
            emptyMessage={t("sets.targets.noDomainsAdded")}
            items={config.targets.sni_domains}
            warning={duplicateWarning}
            onItemsChange={(items) => onChange("targets.sni_domains", items)}
            onInputChange={checkDuplicates}
          />
        </Grid>

        {geo.sitedat_path && geositeCategories.length > 0 && (
          <Grid size={{ sm: 12, md: 6 }}>
            <GeoCategoryPanel
              title={t("sets.targets.geositeCategories")}
              tooltip={t("sets.targets.geositeCatTooltip")}
              inputLabel={t("sets.targets.addGeositeLabel")}
              inputPlaceholder={t("sets.targets.addGeositePlaceholder")}
              helperText={t("sets.targets.geositeCatAvailable", {
                count: geositeCategories.length,
              })}
              activeTitle={t("sets.targets.activeGeositeCategories")}
              options={geositeCategories}
              loading={geositeLoading}
              selected={config.targets.geosite_categories}
              breakdown={stats?.geosite_category_breakdown}
              onSelectedChange={(categories) =>
                onChange("targets.geosite_categories", categories)
              }
              onPreview={setPreviewCategory}
            />
          </Grid>
        )}
      </Grid>

      <CategoryPreviewDialog
        category={previewCategory}
        onClose={() => setPreviewCategory(null)}
      />
    </>
  );
};

import { useState, useRef, useCallback, useEffect, useMemo } from "react";
import { Box, Grid } from "@mui/material";
import { DomainIcon } from "@b4.icons";
import { B4Alert, B4Hint, B4Select, B4Switch } from "@b4.elements";
import { B4SetConfig, GeoConfig } from "@models/config";
import { SetDomainMatch, SetOverlapEntry, SetOverlapView } from "@models/sets";
import { useTranslation } from "react-i18next";
import { SetStats } from "../Manager";
import { ManualEntryPanel } from "./ManualEntryPanel";
import { GeoCategoryPanel } from "./GeoCategoryPanel";
import { CategoryPreviewDialog } from "./CategoryPreviewDialog";
import { OtherSetsTargets, geositeOverlapKey } from "./overlap";
import { useChangedFields } from "@context/ChangedFieldsContext";
import {
  DOMAIN_CATCH_ALL,
  domainEntryNotice,
  isDomainCatchAll,
  normalizeDomainEntry,
  setDomainCatchAll,
} from "./catchall";

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
  otherSetsTargets?: OtherSetsTargets;
  overlaps?: SetOverlapView[];
  geositeCategories: string[];
  geositeLoading: boolean;
  onChange: (
    field: string,
    value: string | string[] | boolean | Record<string, string[]>,
  ) => void;
}

export const DomainsTab = ({
  config,
  geo,
  stats,
  ipv4,
  ipv6,
  otherSetsTargets,
  overlaps,
  geositeCategories,
  geositeLoading,
  onChange,
}: DomainsTabProps) => {
  const { t } = useTranslation();
  const { changedValues } = useChangedFields();
  const showIpVersionFilter = (!!ipv4 && !!ipv6) || !!config.targets.ip_version;
  const [duplicateWarning, setDuplicateWarning] = useState("");
  const [entryNotice, setEntryNotice] = useState("");
  const [previewCategory, setPreviewCategory] = useState<string | null>(null);
  const domains = config.targets.sni_domains ?? [];
  const geositeExclude = config.targets.geosite_exclude ?? {};
  const describeSide = (domain: string, side: SetOverlapEntry) => {
    const source = side.source?.startsWith("geosite:")
      ? t("sets.targets.overlapFromCategory", {
          category: side.source.slice("geosite:".length),
        })
      : t(
          side.source === "manual"
            ? "sets.targets.overlapFromManual"
            : "sets.targets.overlapFromList",
        );
    return side.entry === domain
      ? source
      : t("sets.targets.overlapUnder", { entry: side.entry, source });
  };
  const domainOverlaps = (overlaps ?? [])
    .filter(({ overlap }) => overlap.domains > 0)
    .map(({ otherId, otherName, overlap }) => {
      const own = overlap.set_ids[0] === otherId ? 1 : 0;
      return {
        key: otherId,
        title: t("sets.targets.domainOverlapTitle", {
          count: overlap.domains,
          set: otherName,
        }),
        items: (overlap.examples ?? []).slice(0, 3).map((e) => {
          const goes =
            e.winner_id === config.id
              ? t("sets.targets.overlapGoesHere")
              : t("sets.targets.overlapGoesTo", { set: e.winner_name ?? "" });
          return t("sets.targets.overlapItem", {
            domain: e.domain,
            here: describeSide(e.domain, e.entries[own]),
            set: otherName,
            there: describeSide(e.domain, e.entries[1 - own]),
            goes,
            reason: t(`sets.targets.overlapReason.${e.reason ?? "other"}`),
          });
        }),
      };
    });
  const catchAll = domains.some(isDomainCatchAll);
  const domainsLeaf = changedValues.get("targets.sni_domains");
  const domainsLeafSets = useMemo(() => {
    if (
      !domainsLeaf ||
      !Array.isArray(domainsLeaf.after) ||
      !Array.isArray(domainsLeaf.before)
    ) {
      return null;
    }
    return {
      after: new Set(domainsLeaf.after as unknown[]),
      before: new Set(domainsLeaf.before as unknown[]),
    };
  }, [domainsLeaf]);
  const isDomainChanged = (domain: string): boolean =>
    !!domainsLeafSets &&
    domainsLeafSets.after.has(domain) !== domainsLeafSets.before.has(domain);
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
            Array.isArray(matches)
              ? matches.map((match) => describeMatch(match)).join("; ")
              : "",
          );
        })
        .catch(() => {});
    },
    [config.id, describeMatch],
  );

  const describeNotice = useCallback(
    (input: string) => {
      const notice = domainEntryNotice(input);
      if (!notice) return "";
      if (notice.kind === "catchAll") {
        return t("sets.targets.catchAllDomainNotice", {
          value: notice.values[0],
        });
      }
      return t("sets.targets.wildcardStrippedNotice", {
        from: notice.from,
        to: notice.to,
      });
    },
    [t],
  );

  const checkDuplicates = (input: string) => {
    clearTimeout(checkTimer.current);
    setEntryNotice(describeNotice(input));
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
        <Box sx={{ maxWidth: 360, flex: "1 1 260px", minWidth: 0 }}>
          <B4Switch
            label={t("sets.targets.catchAllDomains")}
            description={t("sets.targets.catchAllDomainsDesc", {
              value: DOMAIN_CATCH_ALL,
            })}
            checked={catchAll}
            path="targets.sni_domains"
            arrayItem={DOMAIN_CATCH_ALL}
            onChange={(checked: boolean) =>
              onChange(
                "targets.sni_domains",
                setDomainCatchAll(domains, checked),
              )
            }
          />
        </Box>
        <Box sx={{ maxWidth: 360, flex: "1 1 260px", minWidth: 0 }}>
          <B4Switch
            description={t("sets.targets.domainOnlyDesc")}
            checked={config.targets.domain_only ?? false}
            path="targets.domain_only"
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
            path="targets.tls"
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
            path="targets.ip_version"
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

      {catchAll && (
        <Grid container sx={{ mb: 2 }}>
          <B4Alert severity="warning">
            {t("sets.targets.catchAllDomainsActive")}
          </B4Alert>
        </Grid>
      )}

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
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
            notice={entryNotice}
            normalize={normalizeDomainEntry}
            highlight={(item) => isDomainCatchAll(item) || isDomainChanged(item)}
            getItemLabel={(item) =>
              isDomainCatchAll(item)
                ? t("sets.targets.catchAllChipDomain")
                : item
            }
            onItemsChange={(items) => onChange("targets.sni_domains", items)}
            onInputChange={checkDuplicates}
          />
        </Grid>

        {geo.sitedat_path && geositeCategories.length > 0 && (
          <Grid size={{ xs: 12, md: 6 }}>
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
              excluded={geositeExclude}
              otherSets={(c) => otherSetsTargets?.get(geositeOverlapKey(c)) ?? []}
              path="targets.geosite_categories"
            />
          </Grid>
        )}
      </Grid>

      {domainOverlaps.length > 0 && (
        <B4Alert severity="warning" sx={{ mt: 2 }}>
          {domainOverlaps.map(({ key, title, items }) => (
            <Box key={key} sx={{ mb: 1 }}>
              {title}
              <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
                {items.map((item) => (
                  <li key={item}>{item}</li>
                ))}
              </Box>
            </Box>
          ))}
          {t("sets.targets.overlapRemoveHint")}
        </B4Alert>
      )}

      <CategoryPreviewDialog
        kind="geosite"
        category={previewCategory}
        excluded={previewCategory ? geositeExclude[previewCategory] : undefined}
        onExcludedChange={(entries) => {
          if (!previewCategory) return;
          onChange("targets.geosite_exclude", {
            ...geositeExclude,
            [previewCategory]: entries,
          });
        }}
        onClose={() => setPreviewCategory(null)}
      />
    </>
  );
};

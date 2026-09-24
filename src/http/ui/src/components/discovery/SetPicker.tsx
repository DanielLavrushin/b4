import { useMemo } from "react";
import { Box, Chip, Stack, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Alert, B4Select } from "@b4.elements";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { ProbeSuggestion } from "@models/discovery";
import { SetDomainMatch } from "@models/sets";
import { MAX_PROBE_URLS, normalizeProbeUrl, probeUrlLabel } from "@utils";

const discoverableSets = (sets: B4SetConfig[]): B4SetConfig[] => {
  const eligible = sets.filter((s) => !s.routing?.enabled);
  const withUrls = eligible.filter((s) => (s.discovery?.urls ?? []).length > 0);
  const without = eligible.filter((s) => (s.discovery?.urls ?? []).length === 0);
  return [...withUrls, ...without];
};

interface SetPickerProps {
  sets: B4SetConfig[];
  value: string | null;
  disabled?: boolean;
  onChange: (setId: string | null) => void;
}

export const SetPicker = ({
  sets,
  value,
  disabled,
  onChange,
}: SetPickerProps) => {
  const { t } = useTranslation();
  const options = useMemo(
    () => [
      { value: "", label: t("discovery.set.none") },
      ...discoverableSets(sets).map((s) => {
        const count = (s.discovery?.urls ?? []).length;
        const parts = [s.name || s.id];
        if (count > 0) parts.push(t("discovery.set.urlCount", { count }));
        if (!s.enabled) parts.push(t("discovery.set.disabled"));
        return { value: s.id, label: parts.join(" · ") };
      }),
    ],
    [sets, t],
  );

  return (
    <Box sx={{ maxWidth: 560 }}>
      <B4Select
        label={t("discovery.set.label")}
        value={value ?? ""}
        options={options}
        disabled={disabled}
        helperText={
          value
            ? t("discovery.set.helper", { max: MAX_PROBE_URLS })
            : t("discovery.set.helperNone")
        }
        onChange={(e) => {
          const next = String(e.target.value);
          onChange(next || null);
        }}
      />
    </Box>
  );
};

interface SetUrlHintsProps {
  set: B4SetConfig;
  urls: string[];
  suggestions: ProbeSuggestion[];
  suggestionsLoaded: boolean;
  owners: SetDomainMatch[];
  disabled?: boolean;
  onAdd: (url: string) => void;
}

interface ForeignOwner {
  host: string;
  owner: string;
}

export const SetUrlHints = ({
  set,
  urls,
  suggestions,
  suggestionsLoaded,
  owners,
  disabled,
  onAdd,
}: SetUrlHintsProps) => {
  const { t } = useTranslation();

  const hosts = useMemo(
    () =>
      new Set(
        urls
          .map((u) => normalizeProbeUrl(u)?.host)
          .filter((h): h is string => !!h),
      ),
    [urls],
  );

  const extra = suggestions.filter((s) => !hosts.has(s.host));

  const foreign = useMemo(() => {
    const list: ForeignOwner[] = [];
    for (const host of hosts) {
      const live = owners.find(
        (m) => m.domain.toLowerCase() === host && m.handles,
      );
      const hint = suggestions.find((s) => s.host === host);
      const ownerId = live ? live.set_id : hint?.owner_set_id;
      const ownerName = live ? live.set_name : hint?.owner_set_name;
      if (!ownerId || ownerId === set.id) continue;
      list.push({ host, owner: ownerName || ownerId });
    }
    return list;
  }, [hosts, owners, suggestions, set.id]);

  const tooMany = urls.length > MAX_PROBE_URLS;
  const empty =
    suggestionsLoaded && suggestions.length === 0 && urls.length === 0;

  return (
    <Stack spacing={1}>
      {extra.length > 0 && (
        <Box>
          <Typography
            variant="caption"
            sx={{ color: colors.text.secondary, display: "block", mb: 0.5 }}
          >
            {t("discovery.set.suggested")}
          </Typography>
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            {extra.map((s) => (
              <Tooltip key={s.url} title={t(`discovery.set.source.${s.source}`)}>
                <Chip
                  size="small"
                  variant="outlined"
                  label={`+ ${probeUrlLabel(s.url)}`}
                  disabled={disabled}
                  onClick={() => onAdd(s.url)}
                  sx={{ cursor: "pointer" }}
                />
              </Tooltip>
            ))}
          </Stack>
        </Box>
      )}
      {empty && (
        <B4Alert severity="info">{t("discovery.set.noSuggestions")}</B4Alert>
      )}
      {foreign.map((f) => (
        <B4Alert key={f.host} severity="warning">
          {t("discovery.set.foreignOwner", {
            host: f.host,
            owner: f.owner,
            name: set.name,
          })}
        </B4Alert>
      ))}
      {tooMany && (
        <B4Alert severity="error">
          {t("discovery.set.tooMany", {
            max: MAX_PROBE_URLS,
            count: urls.length,
          })}
        </B4Alert>
      )}
    </Stack>
  );
};

import { useMemo } from "react";
import { Autocomplete, Box, TextField } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useVoteOrigins } from "./api";
import { asnFallback, asnOption, choose, countryFallback, countryOption, regionNamer, type OriginChoice } from "./origins";

const FIELD_HEIGHT = 25;

const fieldSx = {
  "& .MuiOutlinedInput-root.MuiInputBase-sizeSmall": { height: FIELD_HEIGHT, py: 0, fontSize: 13, bgcolor: colors.background.dark },
  "& .MuiOutlinedInput-root.MuiInputBase-sizeSmall .MuiAutocomplete-input": { py: 0 },
  "&.MuiAutocomplete-hasPopupIcon .MuiOutlinedInput-root, &.MuiAutocomplete-hasClearIcon .MuiOutlinedInput-root": { pr: "26px" },
  "&.MuiAutocomplete-hasPopupIcon.MuiAutocomplete-hasClearIcon .MuiOutlinedInput-root": { pr: "44px" },
  "& .MuiOutlinedInput-root .MuiAutocomplete-endAdornment": { right: 4 },
  "& .MuiOutlinedInput-notchedOutline": { borderColor: colors.border.medium },
  "& .MuiOutlinedInput-notchedOutline legend": { fontSize: "0.85em" },
  "& .MuiOutlinedInput-root:hover .MuiOutlinedInput-notchedOutline": { borderColor: colors.border.strong },
  "& .MuiOutlinedInput-root.Mui-focused .MuiOutlinedInput-notchedOutline": { borderColor: colors.secondary },
  "& .MuiInputLabel-root": { fontSize: 13, color: colors.text.secondary, transform: "translate(14px, 3px) scale(1)" },
  "& .MuiInputLabel-root.MuiInputLabel-shrink": { transform: "translate(14px, -8px) scale(0.85)", maxWidth: "calc(117% - 32px)" },
  "& .MuiInputLabel-root.Mui-focused": { color: colors.secondary },
};

const listboxSx = {
  py: 0.5,
  "& .MuiAutocomplete-option": { px: 1.5, py: 0.75, gap: 2, fontSize: 13 },
  "& .MuiAutocomplete-option.Mui-focused": { bgcolor: colors.accent.secondaryHover },
  "& .MuiAutocomplete-option.Mui-focusVisible": { bgcolor: colors.accent.secondaryHover },
  "& .MuiAutocomplete-option[aria-selected='true']": { bgcolor: colors.accent.secondary },
  "& .MuiAutocomplete-option[aria-selected='true'].Mui-focused": { bgcolor: colors.accent.secondary },
  "& .MuiAutocomplete-option[aria-selected='true'].Mui-focusVisible": { bgcolor: colors.accent.secondary },
};

interface OriginFieldProps {
  label: string;
  choice: OriginChoice;
  loading: boolean;
  width: number;
  popupWidth: number;
  onPick: (key: string | null) => void;
}

function OriginField({ label, choice, loading, width, popupWidth, onPick }: OriginFieldProps) {
  const { t } = useTranslation();
  return (
    <Autocomplete
      size="small"
      autoHighlight
      options={choice.options}
      value={choice.value}
      loading={loading}
      onChange={(_e, o) => onPick(o?.key ?? null)}
      getOptionLabel={(o) => o.label}
      isOptionEqualToValue={(a, b) => a.key === b.key}
      loadingText={t("app.loading")}
      noOptionsText={t("app.noMatches")}
      clearText={t("app.clear")}
      openText={t("app.open")}
      closeText={t("app.close")}
      renderInput={(params) => <TextField {...params} label={label} />}
      renderOption={({ key, ...props }, o) => (
        <li key={key} {...props}>
          <Box component="span" sx={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            <Box component="span" sx={{ fontWeight: 500 }}>
              {o.head}
            </Box>
            {o.detail && (
              <Box component="span" sx={{ ml: 0.75, color: colors.text.secondary }}>
                {o.detail}
              </Box>
            )}
          </Box>
          {o.votes !== undefined && (
            <Box component="span" sx={{ color: colors.text.secondary, fontSize: 12, fontVariantNumeric: "tabular-nums" }}>
              {o.votes}
            </Box>
          )}
        </li>
      )}
      slotProps={{
        popper: { placement: "bottom-start", sx: { minWidth: popupWidth } },
        paper: { elevation: 8, sx: { mt: 0.5, border: `1px solid ${colors.border.default}`, fontSize: 13 } },
        listbox: { sx: listboxSx },
        clearIndicator: { sx: { p: "2px", "& svg": { fontSize: 16 } } },
        popupIndicator: { sx: { p: 0, "& svg": { fontSize: 20 } } },
      }}
      sx={{ width, ...fieldSx }}
    />
  );
}

interface OriginFiltersProps {
  asn: string;
  cc: string;
  onChange: (changes: Record<string, string | null>) => void;
}

export function OriginFilters({ asn, cc, onChange }: OriginFiltersProps) {
  const { t, i18n } = useTranslation();
  const origins = useVoteOrigins();
  const lang = i18n.language;
  const regionName = useMemo(() => regionNamer(lang), [lang]);
  const asns = useMemo(() => (origins.data?.asns ?? []).map(asnOption), [origins.data]);
  const countries = useMemo(() => (origins.data?.countries ?? []).map((m) => countryOption(m, regionName)), [origins.data, regionName]);
  const asnChoice = useMemo(
    () => choose(cc ? asns.filter((o) => o.country === cc) : asns, asn, () => asns.find((o) => o.key === asn) ?? asnFallback(asn)),
    [asns, asn, cc],
  );
  const countryChoice = useMemo(() => choose(countries, cc, () => countryFallback(cc, regionName)), [countries, cc, regionName]);

  const pickCountry = (key: string | null) => {
    const own = asnChoice.value?.country;
    onChange(key && own && own !== key ? { cc: key, asn: null } : { cc: key });
  };

  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5 }}>
      <OriginField label={t("feedback.filters.asn")} choice={asnChoice} loading={origins.isLoading} width={248} popupWidth={360} onPick={(key) => onChange({ asn: key })} />
      <OriginField label={t("feedback.filters.country")} choice={countryChoice} loading={origins.isLoading} width={152} popupWidth={240} onPick={pickCountry} />
    </Box>
  );
}

import { ReactNode, useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  FormControlLabel,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, typography } from "@design";
import { B4Badge } from "@b4.elements";
import { DiscoveryResult } from "@models/discovery";
import {
  HostState,
  OfferedHost,
  hostSkips,
  hostStates,
  unfixedHosts,
} from "@utils";

const PREVIEW = 6;

const captionSx = { color: colors.text.secondary, display: "block" };

interface PageHostsPickerProps {
  header?: ReactNode;
  states: HostState[];
  picks: Record<string, boolean>;
  disabled?: boolean;
  collapse?: boolean;
  onToggle: (host: string, on: boolean) => void;
}

export const PageHostsPicker = ({
  header,
  states,
  picks,
  disabled = false,
  collapse = true,
  onToggle,
}: PageHostsPickerProps) => {
  const { t } = useTranslation();
  const [showAll, setShowAll] = useState(false);

  if (states.length === 0) return null;

  const collapsible = collapse && states.length > PREVIEW;
  const shown = collapsible && !showAll ? states.slice(0, PREVIEW) : states;

  return (
    <Box>
      {header}
      <Stack spacing={0.5} sx={{ mt: 0.75 }}>
        {shown.map((s) => (
          <FormControlLabel
            key={s.host}
            disabled={disabled || s.disabled}
            sx={{ alignItems: "flex-start", mx: 0, gap: 0.5 }}
            control={
              <Checkbox
                size="small"
                checked={!s.disabled && (picks[s.host] ?? s.on)}
                onChange={(e) => onToggle(s.host, e.target.checked)}
                sx={{ p: 0.25 }}
              />
            }
            label={
              <Box sx={{ minWidth: 0, pt: 0.25 }}>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    flexWrap: "wrap",
                    columnGap: 0.75,
                    rowGap: 0.25,
                  }}
                >
                  <Typography
                    component="span"
                    sx={{
                      ...typography.recipes.monoSmall,
                      fontSize: typography.sizes.sm,
                      color: s.disabled
                        ? colors.text.disabled
                        : colors.text.primary,
                      overflowWrap: "anywhere",
                    }}
                  >
                    {s.host}
                  </Typography>
                  {s.context && (
                    <B4Badge
                      variant="outlined"
                      label={t(`discovery.hosts.context.${s.context}`)}
                      sx={{ height: 18, fontSize: typography.sizes.xs }}
                    />
                  )}
                </Box>
                {s.note && (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary, display: "block" }}
                  >
                    {t(s.note.key, s.note.params)}
                  </Typography>
                )}
              </Box>
            }
          />
        ))}
      </Stack>
      {collapsible && (
        <Button
          size="small"
          onClick={() => setShowAll((v) => !v)}
          sx={{ textTransform: "none", mt: 0.5, color: colors.text.secondary }}
        >
          {showAll
            ? t("discovery.results.showFewer")
            : t("discovery.results.showAll", { count: states.length })}
        </Button>
      )}
    </Box>
  );
};

interface CardPageHostsProps {
  domains: string[];
  results: DiscoveryResult[];
  preset: string;
  hosts: OfferedHost[];
  picks: Record<string, boolean>;
  disabled?: boolean;
  onToggle: (host: string, on: boolean) => void;
}

export const CardPageHosts = ({
  domains,
  results,
  preset,
  hosts,
  picks,
  disabled,
  onToggle,
}: CardPageHostsProps) => {
  const { t } = useTranslation();
  const states = hostStates(hosts, [], [], null);
  const unfixed = unfixedHosts(results, preset);
  const skips = hostSkips(results, preset);

  if (states.length === 0 && unfixed.length === 0 && skips.length === 0) {
    return null;
  }

  return (
    <Box
      sx={{ mt: 1.5, pt: 1.5, borderTop: `1px solid ${colors.border.light}` }}
    >
      <PageHostsPicker
        header={
          <>
            <Typography variant="body2" sx={{ color: colors.text.primary }}>
              {t(
                domains.length > 1
                  ? "discovery.hosts.foundShared"
                  : "discovery.hosts.found",
                { count: states.length },
              )}
            </Typography>
            <Typography variant="caption" sx={captionSx}>
              {t("discovery.hosts.hint")}
            </Typography>
          </>
        }
        states={states}
        picks={picks}
        disabled={disabled}
        onToggle={onToggle}
      />
      {(unfixed.length > 0 || skips.length > 0) && (
        <Stack spacing={0.5} sx={{ mt: states.length > 0 ? 1 : 0 }}>
          {unfixed.length > 0 && (
            <Typography variant="caption" sx={captionSx}>
              {t("discovery.hosts.notFixed", { hosts: unfixed.join(", ") })}
            </Typography>
          )}
          {skips.map((skip) => (
            <Typography key={skip.reason} variant="caption" sx={captionSx}>
              {domains.length > 1 && skip.domains.length > 0
                ? `${skip.domains.join(", ")}: `
                : ""}
              {t(`discovery.hosts.skipped.${skip.reason}`)}
            </Typography>
          ))}
        </Stack>
      )}
    </Box>
  );
};

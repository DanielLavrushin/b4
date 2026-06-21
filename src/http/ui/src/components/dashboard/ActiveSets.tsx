import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import {
  Circle as CircleIcon,
  FolderOpen as FolderIcon,
} from "@mui/icons-material";
import { Box, Stack } from "@mui/material";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { DashboardPanel } from "./DashboardPanel";

interface ActiveSetsProps {
  sets: B4SetConfig[];
}

export const ActiveSets = ({ sets }: ActiveSetsProps) => {
  const navigate = useNavigate();
  const { t } = useTranslation();

  if (sets.length === 0) return null;

  return (
    <DashboardPanel eyebrow={t("dashboard.activeSets.title")} padded>
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        {sets.map((set) => {
          const domainCount =
            (set.targets.sni_domains?.length || 0) +
            (set.targets.geosite_categories?.length || 0);
          const ipCount =
            (set.targets.ip?.length || 0) +
            (set.targets.geoip_categories?.length || 0);
          const totalTargets = domainCount + ipCount;

          const goToSet = () => {
            navigate(`/sets/${set.id}`)?.catch(() => {});
          };
          return (
            <Box
              key={set.id}
              role="button"
              tabIndex={0}
              onClick={goToSet}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  goToSet();
                }
              }}
              sx={{
                display: "inline-flex",
                alignItems: "center",
                gap: "6px",
                height: 22,
                padding: "0 10px",
                borderRadius: "11px",
                fontSize: 12,
                fontWeight: 600,
                cursor: "pointer",
                bgcolor: set.enabled
                  ? colors.accent.secondary
                  : colors.accent.primaryStrong,
                color: set.enabled ? colors.secondary : colors.text.disabled,
                transition: "background-color 120ms ease",
                "&:hover": {
                  bgcolor: set.enabled
                    ? colors.accent.secondaryHover
                    : colors.accent.primary,
                },
              }}
            >
              {set.enabled ? (
                <CircleIcon
                  sx={{
                    fontSize: 8,
                    color: colors.state.success,
                    flexShrink: 0,
                  }}
                />
              ) : (
                <FolderIcon
                  sx={{
                    fontSize: 14,
                    color: colors.text.disabled,
                    flexShrink: 0,
                  }}
                />
              )}
              <Box component="span">
                {set.name} · {totalTargets} {t("dashboard.activeSets.targets")}
              </Box>
            </Box>
          );
        })}
      </Stack>
    </DashboardPanel>
  );
};

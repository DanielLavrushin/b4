import { useRef } from "react";
import {
  Box,
  Button,
  LinearProgress,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { colors, radius, radiusPx, spacing, typography } from "@design";
import { useTranslation } from "react-i18next";
import { FACET_COLORS, FACET_ORDER, FacetKey, SetFacet } from "./facets";

const RAIL_REST = 16;
const RAIL_OPEN = 26;
const RAIL_BAR = 5;
const TAB_ICON = 13;
const KEY_COLUMN = 90;

const labelStyle = {
  ...typography.recipes.metricLabel,
  fontWeight: typography.weights.bold,
  color: colors.text.disabled,
};

const valueStyle = {
  ...typography.recipes.monoSmall,
  fontSize: typography.sizes.sm,
};

interface SignalRailProps {
  facets: SetFacet[];
  activeKey: FacetKey | null;
  onSelect: (key: FacetKey) => void;
  onPointerEnter?: () => void;
  expanded?: boolean;
  syncing?: boolean;
}

const expandedRail = {
  height: RAIL_OPEN,
  "& .facet-tab": { height: RAIL_OPEN },
  "& .facet-tab svg": { opacity: 1 },
};

export const SignalRail = ({
  facets,
  activeKey,
  onSelect,
  onPointerEnter,
  expanded,
  syncing,
}: SignalRailProps) => {
  const { t } = useTranslation();
  const railRef = useRef<HTMLDivElement>(null);
  const hasSelection = activeKey !== null;
  const open = hasSelection || !!expanded;

  if (syncing) {
    return (
      <Box sx={{ height: RAIL_REST }}>
        <LinearProgress
          sx={{
            height: RAIL_BAR,
            borderRadius: `${radiusPx.md}px ${radiusPx.md}px 0 0`,
            bgcolor: colors.background.dark,
            "& .MuiLinearProgress-bar": { bgcolor: colors.secondary },
          }}
        />
      </Box>
    );
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const tabs =
      railRef.current?.querySelectorAll<HTMLButtonElement>("button.facet-tab");
    if (!tabs?.length) return;
    const current = Array.from(tabs).indexOf(
      document.activeElement as HTMLButtonElement,
    );
    const step = e.key === "ArrowRight" ? 1 : tabs.length - 1;
    tabs[(Math.max(current, 0) + step) % tabs.length].focus();
  };

  return (
    <Box
      ref={railRef}
      role="tablist"
      aria-label={t("sets.card.f.railLabel")}
      onKeyDown={handleKeyDown}
      onMouseEnter={onPointerEnter}
      sx={{
        display: "grid",
        gridTemplateColumns: `repeat(${facets.length}, 1fr)`,
        gap: "2px",
        alignItems: "start",
        height: RAIL_REST,
        transition: "height 0.18s ease",
        "& .facet-tab": {
          height: RAIL_BAR,
          transition: "height 0.18s ease, opacity 0.15s ease",
        },
        "& .facet-tab svg": {
          fontSize: TAB_ICON,
          opacity: 0,
          transition: "opacity 0.14s ease 0.05s",
        },
        "&:hover": expandedRail,
        "&:focus-within": expandedRail,
        ...(open ? expandedRail : {}),
        "@media (hover: none)": expandedRail,
        "@media (prefers-reduced-motion: reduce)": {
          "& .facet-tab, & .facet-tab svg, &": { transition: "none" },
        },
      }}
    >
      {facets.map((facet, i) => {
        const selected = activeKey === facet.key;
        const first = i === 0;
        const last = i === facets.length - 1;
        return (
          <Tooltip
            key={facet.key}
            title={
              facet.active
                ? facet.label
                : `${facet.label} — ${t("sets.card.f.notConfigured")}`
            }
          >
            <Box
              component="button"
              type="button"
              role="tab"
              className="facet-tab"
              aria-selected={selected}
              tabIndex={selected || (first && !hasSelection) ? 0 : -1}
              onClick={(e: React.MouseEvent) => {
                e.stopPropagation();
                onSelect(facet.key);
              }}
              sx={{
                appearance: "none",
                border: 0,
                p: 0,
                m: 0,
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                color: facet.active
                  ? colors.background.dark
                  : colors.text.disabled,
                bgcolor: facet.active ? facet.color : "rgba(255,232,244,0.09)",
                borderTopLeftRadius: first ? radiusPx.md : 0,
                borderTopRightRadius: last ? radiusPx.md : 0,
                opacity: hasSelection && !selected ? 0.4 : 1,
                "&:focus-visible": {
                  outline: `2px solid ${colors.text.primary}`,
                  outlineOffset: "-3px",
                },
              }}
            >
              {facet.icon}
            </Box>
          </Tooltip>
        );
      })}
    </Box>
  );
};

interface FacetDrawerProps {
  facet: SetFacet;
  onEdit: () => void;
}

export const FacetDrawer = ({ facet, onEdit }: FacetDrawerProps) => {
  const { t } = useTranslation();

  return (
    <Box
      role="tabpanel"
      aria-label={facet.label}
      sx={{
        borderTop: `2px solid ${facet.color}`,
        bgcolor: colors.background.dark,
        px: spacing.md,
        py: spacing.sm,
      }}
    >
      <Stack
        direction="row"
        alignItems="center"
        spacing={spacing.xs}
        sx={{ mb: spacing.sm }}
      >
        <Box
          sx={{
            display: "flex",
            color: facet.color,
            "& svg": { fontSize: TAB_ICON },
          }}
        >
          {facet.icon}
        </Box>
        <Typography
          sx={{
            ...typography.recipes.metricLabel,
            fontWeight: typography.weights.bold,
            color: colors.text.secondary,
          }}
        >
          {facet.label}
        </Typography>
        <Box sx={{ flex: 1 }} />
        <Button
          size="small"
          onClick={(e) => {
            e.stopPropagation();
            onEdit();
          }}
          sx={{
            ...valueStyle,
            minWidth: 0,
            px: spacing.xs,
            py: 0,
            textTransform: "none",
            color: colors.text.disabled,
            "&:hover": { color: colors.text.primary, bgcolor: "transparent" },
          }}
        >
          {t("sets.card.f.edit")} →
        </Button>
      </Stack>

      {facet.active && facet.rows.length > 0 ? (
        <Stack>
          {facet.rows.map((row) => (
            <Box
              key={`${row.label}-${row.value}`}
              sx={{
                display: "grid",
                gridTemplateColumns: `${KEY_COLUMN}px 1fr`,
                gap: spacing.sm,
                alignItems: "center",
                minHeight: 21,
              }}
            >
              <Typography sx={labelStyle}>{row.label}</Typography>
              <Tooltip
                title={`${row.value}${row.muted ? ` ${row.muted}` : ""}`}
              >
                <Typography
                  noWrap
                  sx={{ ...valueStyle, color: colors.text.primary }}
                >
                  {row.value}
                  {row.muted && (
                    <Box
                      component="span"
                      sx={{ color: colors.text.disabled, ml: spacing.xs }}
                    >
                      {row.muted}
                    </Box>
                  )}
                </Typography>
              </Tooltip>
            </Box>
          ))}
        </Stack>
      ) : (
        <Stack
          direction="row"
          alignItems="center"
          spacing={spacing.sm}
          flexWrap="wrap"
          useFlexGap
        >
          <Typography sx={{ ...valueStyle, color: colors.text.disabled }}>
            {t("sets.card.f.notConfigured")}
          </Typography>
          <Button
            size="small"
            variant="outlined"
            onClick={(e) => {
              e.stopPropagation();
              onEdit();
            }}
            sx={{
              ...valueStyle,
              minWidth: 0,
              px: spacing.sm,
              py: 0,
              textTransform: "none",
              borderColor: colors.border.default,
              color: colors.text.secondary,
              "&:hover": {
                borderColor: facet.color,
                color: colors.text.primary,
              },
            }}
          >
            {t("sets.card.f.configure")}
          </Button>
        </Stack>
      )}
    </Box>
  );
};

interface FacetCompareBarProps {
  active: FacetKey | null;
  onPick: (key: FacetKey | null) => void;
}

export const FacetCompareBar = ({ active, onPick }: FacetCompareBarProps) => {
  const { t } = useTranslation();

  return (
    <Stack
      direction="row"
      alignItems="center"
      flexWrap="wrap"
      gap={spacing.sm}
      sx={{
        px: spacing.md,
        py: spacing.sm,
        mb: spacing.md,
        borderRadius: radius.md,
        border: `1px solid ${colors.border.light}`,
        bgcolor: colors.background.paper,
      }}
    >
      <Typography sx={{ ...labelStyle, mr: spacing.xs }}>
        {t("sets.card.f.compare")}
      </Typography>

      {FACET_ORDER.map((key) => {
        const color = FACET_COLORS[key];
        const on = active === key;
        return (
          <Box
            key={key}
            component="button"
            type="button"
            aria-pressed={on}
            onClick={() => onPick(on ? null : key)}
            sx={{
              ...valueStyle,
              display: "inline-flex",
              alignItems: "center",
              gap: spacing.xs,
              cursor: "pointer",
              px: spacing.sm,
              py: spacing.xs / 2,
              borderRadius: `${radiusPx.sm}px`,
              bgcolor: on ? colors.background.control : colors.background.dark,
              border: `1px solid ${on ? color : colors.border.light}`,
              color: on ? colors.text.primary : colors.text.secondary,
              "&:hover": { color: colors.text.primary },
              "&:focus-visible": {
                outline: `2px solid ${colors.secondary}`,
                outlineOffset: "2px",
              },
            }}
          >
            <Box
              sx={{
                width: 8,
                height: 8,
                borderRadius: "50%",
                bgcolor: color,
              }}
            />
            {t(`sets.card.f.${key}`)}
          </Box>
        );
      })}

      <Box
        component="button"
        type="button"
        onClick={() => onPick(null)}
        sx={{
          ...valueStyle,
          cursor: "pointer",
          px: spacing.sm,
          py: spacing.xs / 2,
          borderRadius: `${radiusPx.sm}px`,
          bgcolor: "transparent",
          border: `1px solid ${colors.border.light}`,
          color: colors.text.disabled,
          "&:hover": { color: colors.text.primary },
        }}
      >
        {t("sets.card.f.collapseAll")}
      </Box>
    </Stack>
  );
};

import { Fragment, useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import {
  Box,
  Card,
  CardActionArea,
  CardContent,
  Checkbox,
  Chip,
  Divider,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import MoreVertIcon from "@mui/icons-material/MoreVert";
import { useTranslation } from "react-i18next";
import {
  FacetDrawer,
  SignalRail,
  buildRouteSummary,
  buildSetFacets,
  buildTargetSummary,
  colors,
  facets as facetColors,
  radius,
  spacing,
  typography,
  type FacetKey,
  type FacetSetConfig,
} from "@design";
import type { CardBadge, CardMenuItem, CardPanel, CardVersion, SetCardProps } from "./types";

export const HUB_FACETS: FacetKey[] = ["target", "split", "fake", "route", "dns"];

const RAIL_REST = 16;
const ROUTE_ICON = 14;
const BADGE_ICON = 12;
const RAIL_RELEASE_MS = 260;

const pillStyle = {
  ...typography.recipes.monoSmall,
  fontWeight: typography.weights.bold,
  borderRadius: "999px",
  px: spacing.xs,
  lineHeight: 1.6,
  whiteSpace: "nowrap",
  "&:focus-visible": { outline: `1px solid ${colors.secondary}`, outlineOffset: 2 },
} as const;

const rowStyle = {
  display: "flex",
  flexWrap: "wrap",
  alignItems: "center",
  px: spacing.md,
  py: spacing.sm,
  borderTop: `1px solid ${colors.border.light}`,
} as const;

const separated = { mt: spacing.sm, pt: spacing.sm, borderTop: `1px solid ${colors.border.light}` } as const;

export function VersionPill({ version }: Readonly<{ version: CardVersion }>) {
  const pill = (
    <Typography
      component="span"
      tabIndex={version.tooltip ? 0 : undefined}
      sx={
        version.warning
          ? { ...pillStyle, color: colors.state.warning, border: `1px solid ${colors.state.warning}` }
          : { ...pillStyle, color: colors.background.dark, bgcolor: colors.secondary, border: `1px solid ${colors.secondary}` }
      }
    >
      v{version.version}
    </Typography>
  );
  if (!version.tooltip) return pill;
  return (
    <Tooltip title={version.tooltip} describeChild>
      {pill}
    </Tooltip>
  );
}

function BadgeChip({ badge, pressed, onToggle }: Readonly<{ badge: CardBadge; pressed: boolean; onToggle: (panel: string) => void }>) {
  const panel = badge.panel;
  const action = panel === undefined ? badge.onClick : () => onToggle(panel);
  const focusable = action === undefined && Boolean(badge.tooltip);
  const chip = (
    <Chip
      size="small"
      variant="outlined"
      color={badge.tone ?? "default"}
      icon={badge.icon}
      label={badge.label}
      onClick={action}
      tabIndex={focusable ? 0 : undefined}
      aria-label={badge.ariaLabel ?? (focusable ? badge.label : undefined)}
      aria-pressed={panel === undefined ? undefined : pressed}
      sx={{
        maxWidth: "100%",
        "& .MuiChip-icon": { fontSize: BADGE_ICON, color: "inherit", ml: "8px", mr: "-4px" },
        ...(pressed ? { bgcolor: colors.accent.secondary, borderColor: colors.secondary } : {}),
        "&.Mui-focusVisible, &:focus-visible": { outline: `1px solid ${colors.secondary}`, outlineOffset: 1 },
      }}
    />
  );
  if (!badge.tooltip) return chip;
  return (
    <Tooltip title={badge.tooltip} describeChild>
      {chip}
    </Tooltip>
  );
}

function PanelFrame({ panel, closeLabel, onClose }: Readonly<{ panel: CardPanel; closeLabel: string; onClose: () => void }>) {
  return (
    <Box
      role="region"
      aria-label={panel.label}
      sx={{ borderTop: `1px solid ${colors.border.light}`, bgcolor: colors.background.dark, px: spacing.md, py: spacing.sm, minWidth: 0 }}
    >
      {panel.label && (
        <Stack direction="row" alignItems="center" spacing={spacing.xs} sx={{ mb: spacing.sm, minHeight: 22 }}>
          <Typography sx={{ ...typography.recipes.metricLabel, fontWeight: typography.weights.bold, color: colors.text.secondary, lineHeight: 1.4 }}>
            {panel.label}
          </Typography>
          <Box sx={{ flex: 1 }} />
          <IconButton
            size="small"
            aria-label={closeLabel}
            onClick={onClose}
            sx={{ p: 0.25, color: colors.text.disabled, "&:hover": { color: colors.text.primary } }}
          >
            <CloseIcon sx={{ fontSize: 14 }} />
          </IconButton>
        </Stack>
      )}
      {panel.content}
    </Box>
  );
}

interface CardMenuProps {
  items: CardMenuItem[];
  panel: string | null;
  onToggle: (panel: string) => void;
}

function CardMenu({ items, panel, onToggle }: Readonly<CardMenuProps>) {
  const { t } = useTranslation();
  const id = useId();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const close = () => setAnchor(null);
  return (
    <>
      <IconButton
        size="small"
        aria-label={t("card.menu")}
        aria-haspopup="menu"
        aria-controls={anchor ? id : undefined}
        aria-expanded={anchor ? true : undefined}
        onClick={(e) => setAnchor(e.currentTarget)}
        sx={{ mr: -0.75 }}
      >
        <MoreVertIcon fontSize="small" />
      </IconButton>
      <Menu
        id={id}
        anchorEl={anchor}
        open={anchor !== null}
        onClose={close}
        transformOrigin={{ horizontal: "right", vertical: "top" }}
        anchorOrigin={{ horizontal: "right", vertical: "bottom" }}
      >
        {items.flatMap((item) => {
          const target = item.panel;
          const active = target !== undefined && panel === target;
          const run = () => {
            close();
            if (target !== undefined) onToggle(target);
            item.onClick?.();
          };
          const tint = item.accent ? { color: colors.secondary } : undefined;
          const entry = (
            <MenuItem key={item.key} onClick={run} disabled={item.disabled} sx={tint}>
              {item.icon && <ListItemIcon sx={tint}>{item.icon}</ListItemIcon>}
              <ListItemText>{active && item.activeLabel ? item.activeLabel : item.label}</ListItemText>
            </MenuItem>
          );
          return item.divider ? [<Divider key={`${item.key}-divider`} />, entry] : [entry];
        })}
      </Menu>
    </>
  );
}

function MetaLine({ parts, tooltip }: Readonly<{ parts: string[]; tooltip?: ReactNode }>) {
  const line = (
    <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary, mt: 0.25 }}>
      {parts.map((part, i) => (
        <Fragment key={`${String(i)}-${part}`}>
          {i > 0 && " "}
          <Box
            component="span"
            sx={{ display: "inline-block", maxWidth: "100%", verticalAlign: "top", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}
          >
            {i < parts.length - 1 ? `${part} ·` : part}
          </Box>
        </Fragment>
      ))}
    </Typography>
  );
  return tooltip ? <Tooltip title={tooltip}>{line}</Tooltip> : line;
}

function TargetLine({ text, tooltip }: Readonly<{ text: string; tooltip?: ReactNode }>) {
  return (
    <Tooltip title={tooltip ?? text}>
      <Stack direction="row" alignItems="center" spacing={spacing.xs} sx={{ mt: spacing.xs, minWidth: 0 }}>
        <Box sx={{ width: 7, height: 7, borderRadius: "50%", flexShrink: 0, border: `2px solid ${facetColors.target}` }} />
        <Typography noWrap sx={{ ...typography.recipes.monoSmall, color: colors.text.secondary }}>
          {text}
        </Typography>
      </Stack>
    </Tooltip>
  );
}

function RouteLine({ config }: Readonly<{ config: FacetSetConfig }>) {
  const { t } = useTranslation();
  const route = buildRouteSummary(config, t);
  return (
    <Stack direction="row" alignItems="center" spacing={spacing.xs} sx={{ ...separated, color: route.color, minWidth: 0 }}>
      <Box sx={{ display: "flex", "& svg": { fontSize: ROUTE_ICON } }}>{route.icon}</Box>
      <Tooltip title={route.text}>
        <Typography noWrap sx={{ ...typography.recipes.monoSmall, fontSize: typography.sizes.sm, fontWeight: typography.weights.medium }}>
          {route.text}
        </Typography>
      </Tooltip>
    </Stack>
  );
}

type CardBodyProps = Pick<SetCardProps, "title" | "config" | "targetText" | "targetTooltip" | "meta" | "metaTooltip" | "description" | "extra">;

function CardBody({ title, config, targetText, targetTooltip, meta, metaTooltip, description, extra }: Readonly<CardBodyProps>) {
  const { t } = useTranslation();
  const parts = (meta ?? []).filter(Boolean);
  const target = config ? buildTargetSummary(config, undefined, t) : targetText;
  return (
    <CardContent sx={{ pt: 0, px: spacing.md, pb: spacing.sm, "&:last-child": { pb: spacing.sm }, minWidth: 0 }}>
      <Tooltip title={title}>
        <Typography
          variant="h6"
          component="div"
          noWrap
          sx={{ fontWeight: typography.weights.semibold, textTransform: "uppercase", color: colors.text.primary }}
        >
          {title}
        </Typography>
      </Tooltip>
      {parts.length > 0 && <MetaLine parts={parts} tooltip={metaTooltip} />}
      {target && <TargetLine text={target} tooltip={targetTooltip} />}
      {config ? (
        <RouteLine config={config} />
      ) : (
        <Typography variant="caption" sx={{ ...separated, display: "block", color: colors.text.secondary }}>
          {t("card.unreadable")}
        </Typography>
      )}
      {description && (
        <Tooltip title={description}>
          <Typography
            variant="body2"
            sx={{
              mt: spacing.sm,
              color: colors.text.secondary,
              display: "-webkit-box",
              WebkitBoxOrient: "vertical",
              WebkitLineClamp: 2,
              overflow: "hidden",
              overflowWrap: "anywhere",
            }}
          >
            {description}
          </Typography>
        </Tooltip>
      )}
      {extra}
    </CardContent>
  );
}

function useRailHold() {
  const [expanded, setExpanded] = useState(false);
  const timer = useRef<number | null>(null);
  const cancel = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
  }, []);
  const hold = useCallback(() => {
    cancel();
    setExpanded(true);
  }, [cancel]);
  const release = useCallback(() => {
    cancel();
    timer.current = window.setTimeout(() => {
      timer.current = null;
      setExpanded(false);
    }, RAIL_RELEASE_MS);
  }, [cancel]);
  useEffect(() => cancel, [cancel]);
  return { expanded, hold, release, cancel };
}

function usePanel(controlled: string | null | undefined, onChange?: (panel: string | null) => void) {
  const [local, setLocal] = useState<string | null>(null);
  const panel = controlled === undefined ? local : controlled;
  const set = (next: string | null) => {
    if (controlled === undefined) setLocal(next);
    onChange?.(next);
  };
  return { panel, set, toggle: (key: string) => set(panel === key ? null : key) };
}

export function SetCard({
  setId,
  title,
  config,
  targetText,
  targetTooltip,
  version,
  meta,
  metaTooltip,
  description,
  extra,
  badges = [],
  badgesEnd,
  panels = [],
  panel: controlledPanel,
  onPanelChange,
  menu = [],
  actions,
  selection,
  onOpen,
}: Readonly<SetCardProps>) {
  const { t } = useTranslation();
  const rail = useRailHold();
  const { panel, set: setPanel, toggle: togglePanel } = usePanel(controlledPanel, onPanelChange);

  const facets = config ? buildSetFacets(config, undefined, t, undefined, { hideEmptyDnsServer: true }) : [];
  const openFacet = facets.find((f) => f.key === panel);
  const openPanel = openFacet ? undefined : panels.find((p) => p.key === panel);
  const selected = selection?.selected ?? false;
  const ring = `0 0 0 2px ${colors.secondary}`;
  const lift = `0 8px 24px ${colors.accent.primary}`;
  const body = (
    <CardBody
      title={title}
      config={config}
      targetText={targetText}
      targetTooltip={targetTooltip}
      meta={meta}
      metaTooltip={metaTooltip}
      description={description}
      extra={extra}
    />
  );

  return (
    <Card
      elevation={0}
      data-set-id={setId}
      data-version={version?.version}
      onMouseEnter={rail.cancel}
      onMouseLeave={rail.release}
      sx={{
        position: "relative",
        overflow: "hidden",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        minWidth: 0,
        transition: "border-color 0.2s ease, box-shadow 0.2s ease",
        border: `1px solid ${selected ? colors.secondary : colors.border.default}`,
        borderRadius: radius.md,
        bgcolor: colors.background.paper,
        boxShadow: selected ? ring : "none",
        "&:hover": { borderColor: colors.secondary, boxShadow: selected ? `${ring}, ${lift}` : lift },
      }}
    >
      {config ? (
        <SignalRail
          facets={facets}
          activeKey={openFacet ? openFacet.key : null}
          onSelect={togglePanel}
          onPointerEnter={rail.hold}
          expanded={rail.expanded}
          keys={HUB_FACETS}
          t={t}
        />
      ) : (
        <Box sx={{ height: RAIL_REST }} />
      )}

      <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: spacing.md, pt: spacing.xs / 2, pb: spacing.xs, minHeight: 34 }}>
        {selection && (
          <Checkbox
            size="small"
            checked={selection.selected}
            onChange={selection.onToggle}
            slotProps={{ input: { "aria-label": selection.label } }}
            sx={{ color: colors.text.secondary, "&.Mui-checked": { color: colors.secondary }, p: spacing.xs, ml: -0.75 }}
          />
        )}
        <Box sx={{ flex: 1 }} />
        {version && <VersionPill version={version} />}
        {menu.length > 0 && <CardMenu items={menu} panel={panel} onToggle={togglePanel} />}
      </Box>

      {onOpen ? (
        <CardActionArea
          onClick={onOpen}
          sx={{
            borderRadius: 0,
            flexGrow: 1,
            display: "flex",
            flexDirection: "column",
            alignItems: "stretch",
            justifyContent: "flex-start",
            minWidth: 0,
            "& .MuiCardActionArea-focusHighlight": { display: "none" },
            "&.Mui-focusVisible": { outline: `2px solid ${colors.secondary}`, outlineOffset: -2 },
          }}
        >
          {body}
        </CardActionArea>
      ) : (
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>{body}</Box>
      )}

      {(badges.length > 0 || badgesEnd) && (
        <Box sx={{ ...rowStyle, gap: spacing.xs }}>
          {badges.map((badge) => (
            <BadgeChip key={badge.key} badge={badge} pressed={badge.panel !== undefined && badge.panel === panel} onToggle={togglePanel} />
          ))}
          {badgesEnd && <Box sx={{ ml: "auto", minWidth: 0 }}>{badgesEnd}</Box>}
        </Box>
      )}

      {openFacet && <FacetDrawer facet={openFacet} t={t} />}
      {openPanel && <PanelFrame panel={openPanel} closeLabel={t("app.close")} onClose={() => setPanel(null)} />}

      {actions && <Box sx={{ ...rowStyle, gap: spacing.sm }}>{actions}</Box>}
    </Card>
  );
}

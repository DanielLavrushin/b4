import { useEffect, useRef, useState } from "react";
import {
  Box,
  Card,
  CardActionArea,
  CardContent,
  Checkbox,
  Divider,
  IconButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Stack,
  Switch,
  Tooltip,
  Typography,
} from "@mui/material";
import {
  ClearIcon,
  CompareIcon,
  CopyIcon,
  DragIcon,
  EditIcon,
  EscalateInIcon,
  EscalateOutIcon,
} from "@b4.icons";
import MoreVertIcon from "@mui/icons-material/MoreVert";
import { B4Badge } from "@b4.elements";
import { colors, facets as facetColors, radius, spacing, typography } from "@design";
import { B4SetConfig } from "@models/config";
import { useTranslation } from "react-i18next";
import { SetStats } from "./Manager";
import {
  EditorSection,
  FacetKey,
  buildRouteSummary,
  buildSetFacets,
  buildTargetSummary,
} from "./facets";
import { FacetDrawer, SignalRail } from "./SignalRail";

const ROUTE_ICON = 14;

interface EscalationLink {
  id: string;
  name: string;
}

interface SetCardProps {
  set: B4SetConfig;
  stats?: SetStats;
  index: number;
  onEdit: (section?: EditorSection) => void;
  onDuplicate: () => void;
  onCompare: () => void;
  onDelete: () => void;
  onToggleEnabled: (enabled: boolean) => void;
  syncing?: boolean;
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>;
  selectionMode?: boolean;
  selected?: boolean;
  onSelect?: () => void;
  escalatesTo?: EscalationLink;
  escalatedFrom?: EscalationLink[];
  highlighted?: boolean;
  onEscalationHover?: (setId: string | null) => void;
  onEscalationClick?: (setId: string) => void;
  activeFacet?: FacetKey | null;
  onFacetSelect?: (key: FacetKey) => void;
}

export const SetCard = ({
  set,
  stats,
  index,
  onEdit,
  onDuplicate,
  onCompare,
  onDelete,
  onToggleEnabled,
  syncing,
  dragHandleProps,
  selectionMode,
  selected,
  onSelect,
  escalatesTo,
  escalatedFrom,
  highlighted,
  onEscalationHover,
  onEscalationClick,
  activeFacet = null,
  onFacetSelect,
}: SetCardProps) => {
  const { t } = useTranslation();
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const [railExpanded, setRailExpanded] = useState(false);
  const railTimer = useRef<number | null>(null);

  const cancelRailRelease = () => {
    if (railTimer.current !== null) {
      window.clearTimeout(railTimer.current);
      railTimer.current = null;
    }
  };

  const holdRail = () => {
    cancelRailRelease();
    setRailExpanded(true);
  };

  const releaseRail = () => {
    cancelRailRelease();
    railTimer.current = window.setTimeout(() => {
      railTimer.current = null;
      setRailExpanded(false);
    }, 260);
  };

  useEffect(() => cancelRailRelease, []);

  const isSelected = !!(selectionMode && selected);
  const borderColor =
    highlighted || isSelected ? colors.secondary : colors.border.default;

  const facets = buildSetFacets(set, stats, t, escalatesTo?.name);
  const openFacet = facets.find((f) => f.key === activeFacet);
  const targetSummary = buildTargetSummary(set, stats, t);
  const route = buildRouteSummary(set, t);

  const handleMenuOpen = (e: React.MouseEvent<HTMLElement>) => {
    e.stopPropagation();
    setMenuAnchor(e.currentTarget);
  };

  const handleMenuClose = () => setMenuAnchor(null);

  const handleAction = (action: () => void) => {
    handleMenuClose();
    action();
  };

  return (
    <Card
      elevation={1}
      onMouseEnter={cancelRailRelease}
      onMouseLeave={releaseRail}
      sx={{
        position: "relative",
        overflow: "hidden",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        opacity: set.enabled ? 1 : 0.5,
        transition: "border-color 0.2s ease, box-shadow 0.2s ease",
        border: `1px solid ${borderColor}`,
        borderRadius: radius.md,
        bgcolor: set.enabled ? colors.background.paper : colors.background.dark,
        boxShadow: highlighted
          ? `0 0 0 2px ${colors.secondary}, 0 8px 24px ${colors.accent.primary}`
          : undefined,
        "&:hover": {
          borderColor: colors.secondary,
          boxShadow: `0 8px 24px ${colors.accent.primary}`,
        },
      }}
    >
      <SignalRail
        facets={facets}
        activeKey={activeFacet}
        onSelect={(key) => onFacetSelect?.(key)}
        onPointerEnter={holdRail}
        expanded={railExpanded}
        syncing={syncing}
      />

      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          px: spacing.md,
          pt: spacing.xs / 2,
          pb: spacing.xs,
        }}
      >
        {selectionMode ? (
          <Checkbox
            size="small"
            checked={selected}
            onChange={(e) => {
              e.stopPropagation();
              onSelect?.();
            }}
            onClick={(e) => e.stopPropagation()}
            sx={{
              color: colors.text.secondary,
              "&.Mui-checked": { color: colors.secondary },
              p: spacing.xs,
            }}
          />
        ) : (
          <Box
            {...dragHandleProps}
            sx={{
              cursor: "grab",
              touchAction: "none",
              color: colors.text.secondary,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              minWidth: 32,
              minHeight: 32,
              ml: "-6px",
              "&:hover": { color: colors.secondary },
            }}
          >
            <DragIcon fontSize="small" />
          </Box>
        )}

        <Tooltip title={set.enabled ? t("core.disable") : t("core.enable")}>
          <Switch
            size="small"
            checked={set.enabled}
            onChange={(e) => {
              e.stopPropagation();
              onToggleEnabled(e.target.checked);
            }}
            onClick={(e) => e.stopPropagation()}
          />
        </Tooltip>

        <Typography
          sx={{
            ...typography.recipes.monoSmall,
            fontWeight: typography.weights.bold,
            color: colors.background.dark,
            bgcolor: colors.secondary,
            borderRadius: "999px",
            px: spacing.xs,
            lineHeight: 1.6,
          }}
        >
          {index + 1}
        </Typography>

        <Box sx={{ flex: 1 }} />

        {!selectionMode && (
          <IconButton size="small" onClick={handleMenuOpen}>
            <MoreVertIcon fontSize="small" />
          </IconButton>
        )}

        <Menu
          anchorEl={menuAnchor}
          open={Boolean(menuAnchor)}
          onClose={handleMenuClose}
          transformOrigin={{ horizontal: "right", vertical: "top" }}
          anchorOrigin={{ horizontal: "right", vertical: "bottom" }}
        >
          <MenuItem onClick={() => handleAction(() => onEdit())}>
            <ListItemIcon>
              <EditIcon fontSize="small" />
            </ListItemIcon>
            <ListItemText>{t("core.edit")}</ListItemText>
          </MenuItem>
          <MenuItem onClick={() => handleAction(onDuplicate)}>
            <ListItemIcon>
              <CopyIcon fontSize="small" />
            </ListItemIcon>
            <ListItemText>{t("core.duplicate")}</ListItemText>
          </MenuItem>
          <MenuItem onClick={() => handleAction(onCompare)}>
            <ListItemIcon>
              <CompareIcon fontSize="small" />
            </ListItemIcon>
            <ListItemText>{t("core.compare")}</ListItemText>
          </MenuItem>
          <Divider />
          <MenuItem
            onClick={() => handleAction(onDelete)}
            sx={{ color: colors.secondary }}
          >
            <ListItemIcon>
              <ClearIcon fontSize="small" sx={{ color: colors.secondary }} />
            </ListItemIcon>
            <ListItemText>{t("core.delete")}</ListItemText>
          </MenuItem>
        </Menu>
      </Box>

      <CardActionArea
        onClick={selectionMode ? onSelect : () => onEdit()}
        sx={{
          borderRadius: 0,
          flexGrow: 1,
          display: "flex",
          flexDirection: "column",
          alignItems: "stretch",
          justifyContent: "flex-start",
          "& .MuiCardActionArea-focusHighlight": { display: "none" },
        }}
      >
        <CardContent
          sx={{
            pt: 0,
            px: spacing.md,
            pb: spacing.sm,
            "&:last-child": { pb: spacing.sm },
          }}
        >
          <Typography
            variant="h6"
            noWrap
            sx={{
              fontWeight: typography.weights.semibold,
              textTransform: "uppercase",
              color: set.enabled ? colors.text.primary : colors.text.secondary,
            }}
          >
            {set.name}
          </Typography>

          <Tooltip title={targetSummary}>
            <Stack
              direction="row"
              alignItems="center"
              spacing={spacing.xs}
              sx={{ mt: spacing.xs / 2 }}
            >
              <Box
                sx={{
                  width: 7,
                  height: 7,
                  borderRadius: "50%",
                  flexShrink: 0,
                  border: `2px solid ${facetColors.target}`,
                }}
              />
              <Typography
                noWrap
                sx={{
                  ...typography.recipes.monoSmall,
                  color: colors.text.secondary,
                }}
              >
                {targetSummary}
              </Typography>
            </Stack>
          </Tooltip>

          <Stack
            direction="row"
            alignItems="center"
            spacing={spacing.xs}
            sx={{
              mt: spacing.sm,
              pt: spacing.sm,
              borderTop: `1px solid ${colors.border.light}`,
              color: route.color,
              minWidth: 0,
            }}
          >
            <Box sx={{ display: "flex", "& svg": { fontSize: ROUTE_ICON } }}>
              {route.icon}
            </Box>
            <Tooltip title={route.text}>
              <Typography
                noWrap
                sx={{
                  ...typography.recipes.monoSmall,
                  fontSize: typography.sizes.sm,
                  fontWeight: typography.weights.medium,
                }}
              >
                {route.text}
              </Typography>
            </Tooltip>
          </Stack>
        </CardContent>
      </CardActionArea>

      {(escalatesTo || (escalatedFrom && escalatedFrom.length > 0)) && (
        <Box
          sx={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: spacing.xs,
            px: spacing.md,
            pt: spacing.sm,
            pb: spacing.sm,
            borderTop: `1px solid ${colors.border.light}`,
          }}
        >
          {escalatesTo && (
            <EscalationChip
              icon={<EscalateOutIcon sx={{ fontSize: ESCALATION_ICON }} />}
              prefix={t("sets.card.escalatesTo")}
              link={escalatesTo}
              onHover={onEscalationHover}
              onClick={onEscalationClick}
            />
          )}
          {escalatedFrom?.map((link) => (
            <EscalationChip
              key={link.id}
              icon={<EscalateInIcon sx={{ fontSize: ESCALATION_ICON }} />}
              prefix={t("sets.card.escalatedFrom")}
              link={link}
              onHover={onEscalationHover}
              onClick={onEscalationClick}
              variant="outlined"
            />
          ))}
        </Box>
      )}

      {openFacet && (
        <FacetDrawer
          facet={openFacet}
          onEdit={() => onEdit(openFacet.section)}
        />
      )}
    </Card>
  );
};

interface EscalationChipProps {
  icon: React.ReactNode;
  prefix: string;
  link: { id: string; name: string };
  variant?: "filled" | "outlined";
  onHover?: (setId: string | null) => void;
  onClick?: (setId: string) => void;
}

const ESCALATION_ICON = 12;

const EscalationChip = ({
  icon,
  prefix,
  link,
  variant,
  onHover,
  onClick,
}: EscalationChipProps) => (
  <Tooltip title={`${prefix}: ${link.name}`}>
    <B4Badge
      icon={icon as React.ReactElement}
      label={link.name}
      size="small"
      color="secondary"
      variant={variant}
      clickable
      onMouseEnter={() => onHover?.(link.id)}
      onMouseLeave={() => onHover?.(null)}
      onFocus={() => onHover?.(link.id)}
      onBlur={() => onHover?.(null)}
      onClick={(e) => {
        e.stopPropagation();
        onClick?.(link.id);
      }}
      sx={{
        maxWidth: "100%",
        cursor: "pointer",
        "& .MuiChip-label": {
          overflow: "hidden",
          textOverflow: "ellipsis",
          maxWidth: 110,
        },
      }}
    />
  </Tooltip>
);

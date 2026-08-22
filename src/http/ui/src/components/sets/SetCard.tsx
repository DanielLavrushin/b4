import { useState } from "react";
import {
  Box,
  Card,
  CardContent,
  CardActionArea,
  Checkbox,
  Typography,
  Stack,
  IconButton,
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Switch,
  Tooltip,
  Divider,
  LinearProgress,
} from "@mui/material";
import {
  EditIcon,
  CopyIcon,
  CompareIcon,
  ClearIcon,
  DomainIcon,
  IpIcon,
  DragIcon,
  DnsIcon,
  FakingIcon,
  TcpIcon,
  BlockIcon,
  ProxyIcon,
  NetworkIcon,
  SwapIcon,
  DeviceIcon,
  FilterIcon,
  SecurityIcon,
  EscalateOutIcon,
  EscalateInIcon,
} from "@b4.icons";
import MoreVertIcon from "@mui/icons-material/MoreVert";
import { B4Badge } from "@b4.elements";
import { colors, radius } from "@design";
import { B4SetConfig, RoutingMode } from "@models/config";
import { useTranslation } from "react-i18next";
import { SetStats } from "./Manager";

interface EscalationLink {
  id: string;
  name: string;
}

interface SetCardProps {
  set: B4SetConfig;
  stats?: SetStats;
  index: number;
  onEdit: () => void;
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
}

interface TargetBadgeProps {
  label: string;
  type: "geosite" | "geoip" | "domain" | "ip";
}

const TargetBadge = ({ label, type }: TargetBadgeProps) => {
  // Truncate long labels
  const maxLen = type === "ip" ? 18 : 14;
  const truncated =
    label.length > maxLen ? `${label.slice(0, maxLen)}…` : label;

  const isGeo = type === "geosite" || type === "geoip";

  return (
    <Tooltip title={label}>
      <Box sx={{ maxWidth: 120 }}>
        <B4Badge
          label={truncated}
          size="small"
          icon={
            type === "ip" || type === "geoip" ? (
              <IpIcon sx={{ fontSize: 12 }} />
            ) : undefined
          }
          color={isGeo ? "secondary" : undefined}
          variant={isGeo ? undefined : "outlined"}
          sx={{
            "& .MuiChip-label": {
              overflow: "hidden",
              textOverflow: "ellipsis",
            },
          }}
        />
      </Box>
    </Tooltip>
  );
};

const STRATEGY_LABELS: Record<string, string> = {
  combo: "COMBO",
  hybrid: "HYBRID",
  disorder: "DISORDER",
  overlap: "OVERLAP",
  extsplit: "EXT SPLIT",
  firstbyte: "1ST BYTE",
  tcp: "TCP FRAG",
  ip: "IP FRAG",
  tls: "TLS REC",
  oob: "OOB",
  none: "NONE",
};

type TFn = (key: string) => string;

interface FacetChip {
  key: string;
  label: string;
  icon: React.ReactElement;
  tooltip: string;
  variant?: "filled" | "outlined";
  color?: "default" | "primary" | "secondary" | "info" | "error";
}

const buildScopeChips = (set: B4SetConfig, t: TFn): FacetChip[] => {
  const chips: FacetChip[] = [];
  if (set.targets.tls) {
    chips.push({
      key: "tls",
      label: `TLS ${set.targets.tls}`,
      icon: <SecurityIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.tlsFilter"),
    });
  }
  if (set.tcp?.dport_filter) {
    chips.push({
      key: "tcp-ports",
      label: `TCP ${set.tcp.dport_filter}`,
      icon: <FilterIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.portFilter"),
    });
  }
  if (set.udp?.dport_filter) {
    chips.push({
      key: "udp-ports",
      label: `UDP ${set.udp.dport_filter}`,
      icon: <FilterIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.portFilter"),
    });
  }
  const deviceCount = set.targets.source_devices?.length ?? 0;
  if (deviceCount > 0) {
    chips.push({
      key: "devices",
      label: String(deviceCount),
      icon: <DeviceIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.sourceDevices"),
    });
  }
  if (set.mss_clamp?.enabled) {
    chips.push({
      key: "mss",
      label: `MSS ${set.mss_clamp.size}`,
      icon: <TcpIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.mssClamp"),
    });
  }
  return chips;
};

const resolveRoutingMode = (mode: RoutingMode | undefined): RoutingMode => {
  if (mode === "proxy") return "proxy";
  if (mode === "mtproto-ws") return "mtproto-ws";
  if (mode === "block") return "block";
  return "interface";
};

const buildRouteChips = (set: B4SetConfig, t: TFn): FacetChip[] => {
  const chips: FacetChip[] = [];
  if (set.dns?.enabled) {
    chips.push({
      key: "dns",
      label: set.dns.target_dns ? `DNS → ${set.dns.target_dns}` : "DNS",
      icon: <DnsIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.dnsRedirect"),
      variant: "outlined",
      color: "secondary",
    });
  }
  const routing = set.routing;
  if (!routing?.enabled) return chips;

  const mode = resolveRoutingMode(routing.mode);
  if (mode === "block") {
    chips.push({
      key: "route",
      label: "BLOCK",
      icon: <BlockIcon sx={{ fontSize: 12 }} />,
      tooltip: `${t("sets.card.routeBlock")}: ${routing.block_action || "reject"}`,
      variant: "filled",
      color: "error",
    });
  } else if (mode === "proxy") {
    const up = routing.upstream;
    chips.push({
      key: "route",
      label: up?.host && up?.port ? `→ ${up.host}:${up.port}` : "→ PROXY",
      icon: <ProxyIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.routeProxy"),
      variant: "outlined",
      color: "info",
    });
  } else if (mode === "mtproto-ws") {
    chips.push({
      key: "route",
      label: "MTPROTO-WS",
      icon: <SwapIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.routeMtproto"),
      variant: "outlined",
      color: "info",
    });
  } else {
    chips.push({
      key: "route",
      label: `→ ${routing.egress_interface || "?"}`,
      icon: <NetworkIcon sx={{ fontSize: 12 }} />,
      tooltip: t("sets.card.routeInterface"),
      variant: "outlined",
      color: "info",
    });
  }
  return chips;
};

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
}: SetCardProps) => {
  const { t } = useTranslation();
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const strategy = set.fragmentation.strategy;
  const isSelected = !!(selectionMode && selected);
  const borderColor =
    highlighted || isSelected ? colors.secondary : colors.border.default;

  const domainCount = stats?.total_domains ?? set.targets.sni_domains.length;
  const ipCount = stats?.total_ips ?? set.targets.ip.length;

  const scopeChips = buildScopeChips(set, t);
  const routeChips = buildRouteChips(set, t);

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
      sx={{
        position: "relative",
        overflow: "visible",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        opacity: set.enabled ? 1 : 0.5,
        transition: "all 0.2s ease",
        border: `1px solid ${borderColor}`,
        borderRadius: radius.md,
        bgcolor: set.enabled ? colors.background.paper : colors.background.dark,
        boxShadow: highlighted
          ? `0 0 0 2px ${colors.secondary}, 0 8px 24px ${colors.accent.primary}`
          : undefined,
        "&:hover": {
          borderColor: colors.secondary,
          transform: "translateY(-2px)",
          boxShadow: `0 8px 24px ${colors.accent.primary}`,
        },
      }}
    >
      {/* Top accent bar (becomes a progress indicator while syncing) */}
      {syncing ? (
        <LinearProgress
          sx={{
            height: 4,
            borderRadius: `${radius.md}px ${radius.md}px 0 0`,
            bgcolor: colors.background.dark,
            "& .MuiLinearProgress-bar": { bgcolor: colors.secondary },
          }}
        />
      ) : (
        <Box
          sx={{
            height: 4,
            bgcolor: colors.secondary,
            borderRadius: `${radius.md}px ${radius.md}px 0 0`,
          }}
        />
      )}

      <Box
        sx={{
          position: "absolute",
          top: -9,
          left: -9,
          zIndex: 2,
          minWidth: 24,
          height: 24,
          px: 0.5,
          borderRadius: "12px",
          bgcolor: colors.secondary,
          color: colors.background.dark,
          border: `2px solid ${colors.background.default}`,
          boxShadow: `0 2px 6px rgba(0,0,0,0.45)`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography
          variant="caption"
          fontWeight={800}
          sx={{ fontSize: "0.7rem" }}
        >
          {index + 1}
        </Typography>
      </Box>

      {/* Header row */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          px: 2,
          pt: 1.5,
          pb: 1,
        }}
      >
        <Stack direction="row" alignItems="center" spacing={1}>
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
                p: 0.5,
              }}
            />
          ) : (
            <Box
              {...dragHandleProps}
              sx={{
                cursor: "grab",
                color: colors.text.secondary,
                display: "flex",
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
        </Stack>

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
          <MenuItem onClick={() => handleAction(onEdit)}>
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

      {/* Clickable content area */}
      <CardActionArea
        onClick={selectionMode ? onSelect : onEdit}
        sx={{
          borderRadius: 0,
          flexGrow: 1,
          display: "flex",
          flexDirection: "column",
          alignItems: "stretch",
          "& .MuiCardActionArea-focusHighlight": { display: "none" },
        }}
      >
        <CardContent sx={{ pt: 0, pb: 1.5 }}>
          {/* Name */}
          <Stack
            direction="row"
            alignItems="center"
            spacing={1}
            sx={{ my: 1, minHeight: 30 }}
          >
            <Typography
              variant="h6"
              sx={{
                flex: 1,
                minWidth: 0,
                fontWeight: 600,
                textTransform: "uppercase",
                color: set.enabled
                  ? colors.text.primary
                  : colors.text.secondary,
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
            >
              {set.name}
            </Typography>
            {routeChips.length > 0 && (
              <Stack
                direction="row"
                spacing={0.5}
                sx={{ flexShrink: 0, maxWidth: "55%" }}
              >
                {routeChips.map((chip) => (
                  <Tooltip key={chip.key} title={chip.tooltip}>
                    <B4Badge
                      icon={chip.icon}
                      label={chip.label}
                      size="small"
                      variant={chip.variant}
                      color={chip.color}
                      sx={{
                        maxWidth: 160,
                        "& .MuiChip-label": {
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                        },
                      }}
                    />
                  </Tooltip>
                ))}
              </Stack>
            )}
          </Stack>

          {/* Target preview */}
          <Box
            sx={{
              p: 1.5,
              mb: 1.5,
              borderRadius: radius.sm,
              bgcolor: colors.background.dark,
              border: `1px solid ${colors.border.light}`,
              minHeight: 44,
            }}
          >
            {set.targets.geosite_categories.length > 0 ||
            set.targets.sni_domains.length > 0 ||
            set.targets.geoip_categories.length > 0 ||
            set.targets.ip.length > 0 ? (
              <Stack direction="row" flexWrap="wrap" gap={0.5}>
                {/* Geosite categories first */}
                {set.targets.geosite_categories.slice(0, 2).map((cat) => (
                  <TargetBadge key={cat} label={cat} type="geosite" />
                ))}

                {/* Then domains if room */}
                {set.targets.geosite_categories.length < 2 &&
                  set.targets.sni_domains
                    .slice(0, 2 - set.targets.geosite_categories.length)
                    .map((domain) => (
                      <TargetBadge key={domain} label={domain} type="domain" />
                    ))}

                {/* GeoIP categories */}
                {set.targets.geosite_categories.length +
                  set.targets.sni_domains.length <
                  2 &&
                  set.targets.geoip_categories
                    .slice(
                      0,
                      2 -
                        set.targets.geosite_categories.length -
                        set.targets.sni_domains.length,
                    )
                    .map((cat) => (
                      <TargetBadge key={cat} label={cat} type="geoip" />
                    ))}

                {/* Manual IPs last */}
                {set.targets.geosite_categories.length +
                  set.targets.sni_domains.length +
                  set.targets.geoip_categories.length <
                  2 &&
                  set.targets.ip
                    .slice(
                      0,
                      2 -
                        set.targets.geosite_categories.length -
                        set.targets.sni_domains.length -
                        set.targets.geoip_categories.length,
                    )
                    .map((ip) => <TargetBadge key={ip} label={ip} type="ip" />)}

                {/* +N more */}
                {set.targets.geosite_categories.length +
                  set.targets.sni_domains.length +
                  set.targets.geoip_categories.length +
                  set.targets.ip.length >
                  2 && (
                  <B4Badge
                    label={`+${
                      set.targets.geosite_categories.length +
                      set.targets.sni_domains.length +
                      set.targets.geoip_categories.length +
                      set.targets.ip.length -
                      2
                    }`}
                    size="small"
                    variant="outlined"
                  />
                )}
              </Stack>
            ) : (
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ fontStyle: "italic" }}
              >
                {t("sets.card.noTargets")}
              </Typography>
            )}
          </Box>

          {/* Domain/IP counts */}
          <Stack direction="row" spacing={2} sx={{ mb: 1.5 }}>
            <Tooltip
              title={`${stats?.manual_domains || 0} ${t("sets.card.manual")}, ${
                stats?.geosite_domains || 0
              } ${t("sets.card.geosite")}`}
            >
              <Stack
                direction="row"
                alignItems="center"
                spacing={0.5}
                sx={{ flex: 1 }}
              >
                <DomainIcon
                  sx={{ fontSize: 16, color: colors.text.secondary }}
                />
                <Typography
                  variant="body2"
                  fontWeight={600}
                  color="text.primary"
                >
                  {domainCount.toLocaleString()}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {t("core.domains")}
                </Typography>
              </Stack>
            </Tooltip>
            <Tooltip
              title={`${stats?.manual_ips || 0} ${t("sets.card.manual")}, ${
                stats?.geoip_ips || 0
              } ${t("sets.card.geoip")}`}
            >
              <Stack
                direction="row"
                alignItems="center"
                spacing={0.5}
                sx={{ flex: 1 }}
              >
                <IpIcon sx={{ fontSize: 16, color: colors.text.secondary }} />
                <Typography
                  variant="body2"
                  fontWeight={600}
                  color="text.primary"
                >
                  {ipCount.toLocaleString()}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {t("core.ips")}
                </Typography>
              </Stack>
            </Tooltip>
          </Stack>

          <Stack
            divider={
              <Box sx={{ height: "1px", bgcolor: colors.border.light }} />
            }
            sx={{ borderTop: `1px solid ${colors.border.light}`, mt: 0.5 }}
          >
            <FacetRow label={t("sets.card.facetMethod")}>
              <B4Badge
                label={STRATEGY_LABELS[strategy]}
                size="small"
                sx={{ bgcolor: colors.primary, color: colors.text.primary }}
              />
              {set.faking.sni && (
                <Tooltip title={t("sets.card.sniFakingOn")}>
                  <B4Badge
                    icon={<FakingIcon sx={{ fontSize: 12 }} />}
                    label="FAKE"
                    size="small"
                    color="primary"
                  />
                </Tooltip>
              )}
              {set.fragmentation.reverse_order && (
                <B4Badge label="REV" size="small" variant="outlined" />
              )}
            </FacetRow>

            {scopeChips.length > 0 && (
              <FacetRow label={t("sets.card.facetScope")}>
                {scopeChips.map((chip) => (
                  <Tooltip key={chip.key} title={chip.tooltip}>
                    <B4Badge
                      icon={chip.icon}
                      label={chip.label}
                      size="small"
                      variant="outlined"
                    />
                  </Tooltip>
                ))}
              </FacetRow>
            )}
          </Stack>
        </CardContent>
      </CardActionArea>

      {(escalatesTo || (escalatedFrom && escalatedFrom.length > 0)) && (
        <Box
          sx={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            gap: 0.5,
            px: 2,
            pt: 1,
            pb: 1.25,
          }}
        >
          {escalatesTo && (
            <EscalationChip
              icon={<EscalateOutIcon sx={{ fontSize: 12 }} />}
              prefix={t("sets.card.escalatesTo")}
              link={escalatesTo}
              onHover={onEscalationHover}
              onClick={onEscalationClick}
            />
          )}
          {escalatedFrom?.map((link) => (
            <EscalationChip
              key={link.id}
              icon={<EscalateInIcon sx={{ fontSize: 12 }} />}
              prefix={t("sets.card.escalatedFrom")}
              link={link}
              onHover={onEscalationHover}
              onClick={onEscalationClick}
              variant="outlined"
            />
          ))}
        </Box>
      )}
    </Card>
  );
};

interface FacetRowProps {
  label: string;
  children: React.ReactNode;
}

const FacetRow = ({ label, children }: FacetRowProps) => (
  <Stack direction="row" alignItems="center" sx={{ py: 0.875, minHeight: 34 }}>
    <Typography
      variant="caption"
      sx={{
        color: colors.text.disabled,
        fontWeight: 700,
        fontSize: "0.6rem",
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        width: 56,
        flexShrink: 0,
        borderRight: `1px solid ${colors.border.light}`,
        mr: 1.5,
      }}
    >
      {label}
    </Typography>
    <Stack
      direction="row"
      flexWrap="wrap"
      gap={0.5}
      sx={{ flex: 1, minWidth: 0 }}
    >
      {children}
    </Stack>
  </Stack>
);

interface EscalationChipProps {
  icon: React.ReactNode;
  prefix: string;
  link: { id: string; name: string };
  variant?: "filled" | "outlined";
  onHover?: (setId: string | null) => void;
  onClick?: (setId: string) => void;
}

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

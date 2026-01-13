import {
  IconArrowsExchange,
  CheckIcon,
  CopyIcon,
  IconDotsVertical,
  EditIcon,
  DragIcon,
  IpIcon,
  ClearIcon,
  DomainIcon,
  CloseIcon,
} from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
} from "@design/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@design/components/ui/dropdown-menu";
import { Switch } from "@design/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { cn } from "@design/lib/utils";
import { B4SetConfig, MAIN_SET_ID } from "@models/config";
import { useMemo, useState } from "react";
import { SetStats } from "./Manager";

interface SetCardProps {
  set: B4SetConfig;
  stats?: SetStats;
  index: number;
  onEdit: () => void;
  onDuplicate: () => void;
  onCompare: () => void;
  onDelete: () => void;
  onToggleEnabled: (enabled: boolean) => void;
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>;
}

interface TargetBadgeProps {
  label: string;
  type: "geosite" | "geoip" | "domain" | "ip";
}

const TargetBadge = ({ label, type }: TargetBadgeProps) => {
  const maxLen = type === "ip" ? 18 : 14;
  const truncated =
    label.length > maxLen ? `${label.slice(0, maxLen)}…` : label;
  const isGeo = type === "geosite" || type === "geoip";

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge
          variant={isGeo ? "secondary" : "outline"}
          className="inline-flex items-center gap-1"
        >
          {(type === "ip" || type === "geoip") && (
            <IpIcon className="h-3 w-3" />
          )}
          {truncated}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>
        <p>{label}</p>
      </TooltipContent>
    </Tooltip>
  );
};

const STRATEGY_LABELS: Record<string, string> = {
  combo: "COMBO",
  hybrid: "HYBRID",
  disorder: "DISORDER",
  extsplit: "EXT SPLIT",
  firstbyte: "1ST BYTE",
  tcp: "TCP FRAG",
  ip: "IP FRAG",
  tls: "TLS REC",
  oob: "OOB",
  none: "NONE",
};

const QUIC_FILTER_LABELS: Record<string, string> = {
  disabled: "QUIC",
  all: "ALL",
  parse: "PARSE",
};

const FAKE_STRATEGY_LABELS: Record<string, string> = {
  ttl: "TTL",
  randseq: "RANDSEQ",
  pastseq: "PASTSEQ",
  tcp_check: "TCP CHECK",
  md5sum: "MD5SUM",
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
  dragHandleProps,
}: SetCardProps) => {
  const [menuOpen, setMenuOpen] = useState(false);
  const isMain = set.id === MAIN_SET_ID;
  const strategy = set.fragmentation.strategy;

  const domainCount = stats?.total_domains ?? set.targets.sni_domains.length;
  const ipCount = stats?.total_ips ?? set.targets.ip.length;

  // Calculate total targets count
  const totalTargets = useMemo(() => {
    return (
      set.targets.geosite_categories.length +
      set.targets.sni_domains.length +
      set.targets.geoip_categories.length +
      set.targets.ip.length
    );
  }, [set.targets]);

  // Get preview targets (max 2)
  const previewTargets = useMemo(() => {
    const targets: Array<{
      label: string;
      type: "geosite" | "geoip" | "domain" | "ip";
    }> = [];

    // Geosite categories
    set.targets.geosite_categories.slice(0, 2).forEach((cat) => {
      if (targets.length < 2) targets.push({ label: cat, type: "geosite" });
    });

    // Domains
    if (targets.length < 2) {
      set.targets.sni_domains.slice(0, 2 - targets.length).forEach((domain) => {
        targets.push({ label: domain, type: "domain" });
      });
    }

    // GeoIP categories
    if (targets.length < 2) {
      set.targets.geoip_categories
        .slice(0, 2 - targets.length)
        .forEach((cat) => {
          targets.push({ label: cat, type: "geoip" });
        });
    }

    // IPs
    if (targets.length < 2) {
      set.targets.ip.slice(0, 2 - targets.length).forEach((ip) => {
        targets.push({ label: ip, type: "ip" });
      });
    }

    return targets;
  }, [set.targets]);

  const handleAction = (action: () => void) => {
    setMenuOpen(false);
    action();
  };

  return (
    <Card
      className={cn(
        "relative flex flex-row transition-all",
        set.enabled ? "opacity-100" : "opacity-50",
        "hover:shadow-lg",
        isMain && "border-primary"
      )}
    >
      {/* Left accent bar */}
      <div
        className={cn(
          "w-1 rounded-l-lg shrink-0",
          isMain ? "bg-primary" : "bg-secondary"
        )}
      />

      {/* Drag handle */}
      <div
        {...dragHandleProps}
        className="cursor-grab text-muted-foreground hover:text-foreground transition-colors shrink-0 flex items-center self-center"
      >
        <DragIcon />
      </div>

      {/* Main content */}
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2 flex-1 min-w-0">
              <Tooltip>
                <TooltipTrigger asChild>
                  <div
                    onClick={(e) => e.stopPropagation()}
                    className="shrink-0"
                  >
                    <Switch
                      checked={set.enabled}
                      onCheckedChange={onToggleEnabled}
                    />
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{set.enabled ? "Disable" : "Enable"}</p>
                </TooltipContent>
              </Tooltip>

              {isMain && (
                <Badge variant="secondary" className="shrink-0">
                  MAIN
                </Badge>
              )}

              {/* Name */}
              <h6
                className={cn(
                  "font-semibold uppercase text-sm truncate",
                  set.enabled ? "text-foreground" : "text-muted-foreground"
                )}
              >
                {set.name}
              </h6>
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <Button
                size="sm"
                variant="secondary"
                onClick={(e) => {
                  e.stopPropagation();
                  handleAction(onEdit);
                }}
              >
                <EditIcon className="h-4 w-4 mr-2" />
                Edit
              </Button>
              <DropdownMenu
                open={menuOpen}
                onOpenChange={setMenuOpen}
                modal={false}
              >
                <DropdownMenuTrigger asChild>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <IconDotsVertical />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  align="end"
                  onClick={(e) => e.stopPropagation()}
                >
                  <DropdownMenuItem onClick={() => handleAction(onDuplicate)}>
                    <CopyIcon className="h-4 w-4 mr-2" />
                    Duplicate
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleAction(onCompare)}>
                    <IconArrowsExchange className="h-4 w-4 mr-2" />
                    Compare
                  </DropdownMenuItem>
                  {!isMain && <DropdownMenuSeparator />}
                  {!isMain && (
                    <DropdownMenuItem
                      onClick={() => handleAction(onDelete)}
                      className="text-destructive"
                    >
                      <ClearIcon className="h-4 w-4 mr-2" />
                      Delete
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </CardHeader>

        {/* Content */}
        <CardContent className="flex-1 flex flex-row gap-6 items-center">
          {/* Target preview */}
          <div style={{ flex: "1 1 40%", minWidth: 0 }}>
            <div className="p-3 rounded-md bg-muted border border-border flex items-center">
              {totalTargets > 0 ? (
                <div className="flex flex-wrap gap-1.5 w-full">
                  {previewTargets.map((target) => (
                    <TargetBadge
                      key={`${target.type}-${target.label}`}
                      label={target.label}
                      type={target.type}
                    />
                  ))}
                  {totalTargets > 2 && (
                    <Badge variant="outline">+{totalTargets - 2}</Badge>
                  )}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground italic w-full text-center">
                  No targets configured
                </p>
              )}
            </div>
          </div>

          {/* Domain/IP counts */}
          <div
            className="flex flex-col gap-1 shrink-0"
            style={{ flex: "0 0 20%" }}
          >
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="flex items-center gap-1.5 w-fit">
                  <DomainIcon className="h-4 w-4 text-muted-foreground shrink-0" />
                  <span className="text-sm font-semibold text-foreground">
                    {domainCount.toLocaleString()}
                  </span>
                  <span className="text-xs text-muted-foreground">domains</span>
                </div>
              </TooltipTrigger>
              <TooltipContent>
                <p>
                  {stats?.manual_domains || 0} manual,{" "}
                  {stats?.geosite_domains || 0} geosite
                </p>
              </TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <div className="flex items-center gap-1.5 w-fit">
                  <IpIcon className="h-4 w-4 text-muted-foreground shrink-0" />
                  <span className="text-sm font-semibold text-foreground">
                    {ipCount.toLocaleString()}
                  </span>
                  <span className="text-xs text-muted-foreground">IPs</span>
                </div>
              </TooltipTrigger>
              <TooltipContent>
                <p>
                  {stats?.manual_ips || 0} manual, {stats?.geoip_ips || 0} geoip
                </p>
              </TooltipContent>
            </Tooltip>
          </div>

          {/* Combined techniques and flags */}
          <div style={{ flex: "1 1 40%", minWidth: 0 }}>
            <div className="flex flex-wrap gap-1.5">
              {/* Fragmentation Badge */}
              <Badge
                variant={strategy !== "none" ? "default" : "ghost"}
                className="text-xs shrink-0"
              >
                {STRATEGY_LABELS[strategy] || strategy.toUpperCase()}
              </Badge>

              {/* QUIC Filter Badge */}
              <Badge
                variant={
                  set.udp.filter_quic !== "disabled" ? "default" : "ghost"
                }
                className="text-xs shrink-0"
              >
                {QUIC_FILTER_LABELS[set.udp.filter_quic] || "QUIC"}
              </Badge>

              {/* DNS Redirect Badge */}
              <Badge
                variant={set.dns?.enabled ? "default" : "ghost"}
                className="text-xs shrink-0 max-w-full truncate"
              >
                {set.dns?.enabled ? set.dns.target_dns || "DNS" : "DNS"}
              </Badge>

              {/* Fake SNI Badge */}
              <Badge
                variant={set.faking.sni ? "default" : "ghost"}
                className="text-xs shrink-0"
              >
                {set.faking.sni
                  ? FAKE_STRATEGY_LABELS[set.faking.strategy] ||
                    set.faking.strategy.toUpperCase()
                  : "FAKE"}
              </Badge>
            </div>
          </div>
        </CardContent>
      </div>
    </Card>
  );
};

interface QuickFlagProps {
  icon: React.ReactNode;
  label?: string;
  enabled?: boolean;
  tooltip: string;
}

const QuickFlag = ({ icon, label, enabled, tooltip }: QuickFlagProps) => {
  const isActive = enabled !== undefined ? enabled : true;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div
          className={cn(
            "flex items-center gap-1.5 flex-1 justify-center py-1.5 px-2 rounded-md transition-colors",
            isActive ? "bg-accent" : "bg-transparent"
          )}
        >
          <div
            className={cn(
              isActive ? "text-foreground" : "text-muted-foreground"
            )}
          >
            {icon}
          </div>
          {label ? (
            <span
              className={cn(
                "text-xs font-semibold",
                isActive ? "text-foreground" : "text-muted-foreground"
              )}
            >
              {label}
            </span>
          ) : (
            enabled !== undefined && (
              <div
                className={cn(
                  isActive ? "text-foreground" : "text-muted-foreground"
                )}
              >
                {enabled ? (
                  <CheckIcon className="h-3 w-3" />
                ) : (
                  <CloseIcon className="h-3 w-3" />
                )}
              </div>
            )
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <p>{tooltip}</p>
      </TooltipContent>
    </Tooltip>
  );
};

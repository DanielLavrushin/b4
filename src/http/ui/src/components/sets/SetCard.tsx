import {
  ClearIcon,
  CopyIcon,
  DomainIcon,
  DragIcon,
  EditIcon,
  IconArrowsExchange,
  IconDotsVertical,
  IpIcon,
} from "@b4.icons";
import { cn } from "@design/lib/utils";
import { B4SetConfig, MAIN_SET_ID } from "@models/config";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Card, CardContent, CardHeader, CardTitle } from "@primitives/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@primitives/dropdown-menu";
import { Switch } from "@primitives/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
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
      <TooltipTrigger>
        <Badge
          variant={isGeo ? "secondary" : "outline"}
          className="inline-flex items-center gap-1"
        >
          {(type === "ip" || type === "geoip") && <IpIcon className="size-3" />}
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
        "flex flex-row transition-all",
        set.enabled ? "opacity-100" : "opacity-50",
        "hover:shadow-lg",
      )}
    >
      {/* Left accent bar */}
      <div
        className={cn("w-1 shrink-0", isMain ? "bg-primary" : "bg-secondary")}
      />

      {/* Drag handle */}
      <div
        {...dragHandleProps}
        className="text-muted-foreground hover:text-foreground flex shrink-0 cursor-grab self-center transition-colors"
      >
        <DragIcon />
      </div>

      {/* Main content */}
      <div className="flex flex-1 flex-col">
        {/* Header */}
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <Tooltip>
                <TooltipTrigger>
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
              <CardTitle
                className={cn(
                  set.enabled ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {set.name}
              </CardTitle>
            </div>

            <div className="flex shrink-0 items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={(e) => {
                  e.stopPropagation();
                  handleAction(onEdit);
                }}
              >
                <EditIcon />
                Edit
              </Button>
              <DropdownMenu
                open={menuOpen}
                onOpenChange={setMenuOpen}
                modal={false}
              >
                <DropdownMenuTrigger>
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
                    <CopyIcon className="mr-2 size-4" />
                    Duplicate
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleAction(onCompare)}>
                    <IconArrowsExchange className="mr-2 size-4" />
                    Compare
                  </DropdownMenuItem>
                  {!isMain && <DropdownMenuSeparator />}
                  {!isMain && (
                    <DropdownMenuItem
                      onClick={() => handleAction(onDelete)}
                      className="text-destructive"
                    >
                      <ClearIcon className="mr-2 size-4" />
                      Delete
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </CardHeader>

        {/* Content */}
        <CardContent className="flex flex-1 flex-row items-center gap-6">
          {/* Target preview */}
          <div className="bg-muted border-border flex-1/3 items-center border p-3">
            {totalTargets > 0 ? (
              <div className="flex flex-wrap gap-1.5">
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
              <p className="text-muted-foreground w-full text-center text-xs italic">
                No targets configured
              </p>
            )}
          </div>

          {/* Domain/IP counts */}
          <div
            className="flex shrink-0 flex-col gap-1"
            style={{ flex: "0 0 20%" }}
          >
            <Tooltip>
              <TooltipTrigger>
                <div className="flex w-fit items-center gap-1.5">
                  <DomainIcon className="text-muted-foreground size-4 shrink-0" />
                  <span className="text-foreground text-sm font-semibold">
                    {domainCount.toLocaleString()}
                  </span>
                  <span className="text-muted-foreground text-xs">domains</span>
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
              <TooltipTrigger>
                <div className="flex w-fit items-center gap-1.5">
                  <IpIcon className="text-muted-foreground size-4 shrink-0" />
                  <span className="text-foreground text-sm font-semibold">
                    {ipCount.toLocaleString()}
                  </span>
                  <span className="text-muted-foreground text-xs">IPs</span>
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
                variant={strategy === "none" ? "ghost" : "default"}
                className="shrink-0 text-xs"
              >
                {STRATEGY_LABELS[strategy] || strategy.toUpperCase()}
              </Badge>

              {/* QUIC Filter Badge */}
              <Badge
                variant={
                  set.udp.filter_quic === "disabled" ? "ghost" : "default"
                }
                className="shrink-0 text-xs"
              >
                {QUIC_FILTER_LABELS[set.udp.filter_quic] || "QUIC"}
              </Badge>

              {/* DNS Redirect Badge */}
              <Badge
                variant={set.dns?.enabled ? "default" : "ghost"}
                className="max-w-full shrink-0 truncate text-xs"
              >
                {set.dns?.enabled ? set.dns.target_dns || "DNS" : "DNS"}
              </Badge>

              {/* Fake SNI Badge */}
              <Badge
                variant={set.faking.sni ? "default" : "ghost"}
                className="shrink-0 text-xs"
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

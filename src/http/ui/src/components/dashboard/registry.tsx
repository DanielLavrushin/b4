import type { ReactNode } from "react";
import { B4SetConfig } from "@models/config";
import { ActiveSets } from "./ActiveSets";
import { Blackhole } from "./Blackhole";
import { DeviceActivity } from "./DeviceActivity";
import { Escalations } from "./Escalations";
import { LiveSignal } from "./LiveSignal";
import { MTProtoActivity } from "./MTProtoActivity";
import { RuntimeHealth } from "./RuntimeHealth";
import { UnmatchedDomains } from "./UnmatchedDomains";
import type { Metrics } from "./types";

export const GRID_COLUMNS = 12;

export const MIN_SPAN = 3;

export interface PanelContext {
  metrics: Metrics;
  sets: B4SetConfig[];
  targetedDomains: Set<string>;
  refreshSets: () => void;
}

export interface PanelDescriptor {
  id: string;
  titleKey: string;
  defaultSpan: (ctx: PanelContext) => number;
  available: (ctx: PanelContext) => boolean;
  render: (ctx: PanelContext) => ReactNode;
}

export const DASHBOARD_PANELS: PanelDescriptor[] = [
  {
    id: "runtime",
    titleKey: "dashboard.runtime.title",
    defaultSpan: () => 12,
    available: () => true,
    render: ({ metrics }) => <RuntimeHealth metrics={metrics} />,
  },
  {
    id: "signal",
    titleKey: "dashboard.signal.title",
    defaultSpan: () => 12,
    available: () => true,
    render: ({ metrics }) => <LiveSignal metrics={metrics} />,
  },
  {
    id: "activeSets",
    titleKey: "dashboard.activeSets.title",
    defaultSpan: () => 12,
    available: ({ sets }) => sets.length > 0,
    render: ({ sets }) => <ActiveSets sets={sets} />,
  },
  {
    id: "deviceActivity",
    titleKey: "dashboard.deviceActivity.title",
    defaultSpan: () => 6,
    available: ({ metrics }) =>
      Object.keys(metrics.device_domains).length > 0,
    render: ({ metrics, sets, targetedDomains, refreshSets }) => (
      <DeviceActivity
        deviceDomains={metrics.device_domains}
        domainTLS={metrics.domain_tls}
        sets={sets}
        targetedDomains={targetedDomains}
        onRefreshSets={refreshSets}
      />
    ),
  },
  {
    id: "unmatchedDomains",
    titleKey: "dashboard.unmatchedDomains.title",
    defaultSpan: ({ metrics }) =>
      Object.keys(metrics.device_domains).length > 0 ? 6 : 12,
    available: () => true,
    render: ({ metrics, sets, targetedDomains, refreshSets }) => (
      <UnmatchedDomains
        topDomains={metrics.top_domains}
        domainTLS={metrics.domain_tls}
        sets={sets}
        targetedDomains={targetedDomains}
        onRefreshSets={refreshSets}
      />
    ),
  },
  {
    id: "mtproto",
    titleKey: "dashboard.mtproto.title",
    defaultSpan: () => 12,
    available: ({ metrics }) => Boolean(metrics.mtproto?.enabled),
    render: ({ metrics }) =>
      metrics.mtproto ? <MTProtoActivity stats={metrics.mtproto} /> : null,
  },
  {
    id: "escalations",
    titleKey: "dashboard.escalations.title",
    defaultSpan: () => 12,
    available: ({ metrics }) => metrics.escalations.length > 0,
    render: ({ metrics }) => (
      <Escalations
        escalations={metrics.escalations}
        total={metrics.total_escalations}
      />
    ),
  },
  {
    id: "blackhole",
    titleKey: "dashboard.blackhole.title",
    defaultSpan: () => 12,
    available: ({ metrics }) => metrics.blocked_total > 0,
    render: ({ metrics }) => (
      <Blackhole
        total={metrics.blocked_total}
        blockedDomains={metrics.blocked_domains}
        blockedDevices={metrics.blocked_devices}
      />
    ),
  },
];

export const PANELS_BY_ID = new Map(DASHBOARD_PANELS.map((p) => [p.id, p]));

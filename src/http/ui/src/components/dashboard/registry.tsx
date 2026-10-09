import type { ComponentType } from "react";
import {
  ActivityPanel,
  BlockedPanel,
  DomainsPanel,
  EscalationsPanel,
  EventsPanel,
  SetsPanel,
  TelegramPanel,
  type PanelAvailability,
} from "./panels";

export const GRID_COLUMNS = 12;

export const MIN_SPAN = 3;

export type PanelCondition = keyof PanelAvailability;

export interface PanelDescriptor {
  id: string;
  titleKey: string;
  defaultSpan: number;
  when?: PanelCondition;
  Component: ComponentType;
}

export const DASHBOARD_PANELS: readonly PanelDescriptor[] = [
  {
    id: "activity",
    titleKey: "dashboard.activity.title",
    defaultSpan: 12,
    Component: ActivityPanel,
  },
  {
    id: "sets",
    titleKey: "dashboard.sets.title",
    defaultSpan: 8,
    Component: SetsPanel,
  },
  {
    id: "events",
    titleKey: "dashboard.events.title",
    defaultSpan: 4,
    Component: EventsPanel,
  },
  {
    id: "domains",
    titleKey: "dashboard.domains.title",
    defaultSpan: 6,
    Component: DomainsPanel,
  },
  {
    id: "escalations",
    titleKey: "dashboard.escalations.title",
    defaultSpan: 6,
    when: "escalations",
    Component: EscalationsPanel,
  },
  {
    id: "blocked",
    titleKey: "dashboard.blocked.title",
    defaultSpan: 6,
    when: "blocked",
    Component: BlockedPanel,
  },
  {
    id: "telegram",
    titleKey: "dashboard.telegram.title",
    defaultSpan: 12,
    when: "telegram",
    Component: TelegramPanel,
  },
];

export const PANELS_BY_ID = new Map(DASHBOARD_PANELS.map((p) => [p.id, p]));

export const isPanelAvailable = (
  panel: PanelDescriptor,
  availability: PanelAvailability,
): boolean => (panel.when ? availability[panel.when] : true);

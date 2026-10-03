import type { ReactElement, ReactNode } from "react";
import type { FacetSetConfig } from "@design";
import type { StatusTone } from "@/shared/components/StatusDot";

export type CardTone = "default" | "secondary" | "warning" | "error";

export interface CardBadge {
  key: string;
  label: string;
  tone?: CardTone;
  filled?: boolean;
  icon?: ReactElement;
  tooltip?: ReactNode;
  panel?: string;
  onClick?: () => void;
}

export interface CardMenuItem {
  key: string;
  label: string;
  activeLabel?: string;
  icon?: ReactElement;
  panel?: string;
  onClick?: () => void;
  divider?: boolean;
  accent?: boolean;
  disabled?: boolean;
}

export interface CardPanel {
  key: string;
  label?: string;
  content: ReactNode;
}

export interface CardVersion {
  version: number;
  tooltip?: ReactNode;
  warning?: boolean;
}

export interface CardStatus {
  tone: StatusTone;
  label: string;
  tooltip?: ReactNode;
}

export interface CardSelection {
  selected: boolean;
  onToggle: () => void;
  label: string;
}

export interface SetCardProps {
  setId: string;
  title: string;
  config?: FacetSetConfig;
  targetText?: string;
  version?: CardVersion;
  status?: CardStatus;
  meta?: string[];
  metaTooltip?: ReactNode;
  description?: string;
  extra?: ReactNode;
  badges?: CardBadge[];
  badgesEnd?: ReactNode;
  panels?: CardPanel[];
  panel?: string | null;
  onPanelChange?: (panel: string | null) => void;
  menu?: CardMenuItem[];
  actions?: ReactNode;
  selection?: CardSelection;
  onOpen?: () => void;
}

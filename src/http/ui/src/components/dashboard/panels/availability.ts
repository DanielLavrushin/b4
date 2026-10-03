import { useMemo } from "react";
import type { MetricsFrame } from "@models/metrics";
import { useTelegramBridgeEnabled } from "@hooks/useTelegramBridge";
import { shallowEqual, useMetricsFrame } from "@/stores/useMetrics";

export const hasEscalations = (frame: MetricsFrame): boolean =>
  (frame.escalations?.items.length ?? 0) > 0;

export const hasBlocking = (frame: MetricsFrame): boolean =>
  frame.totals.blocked_dns + frame.totals.blocked_conns > 0 ||
  frame.sets.some((set) => set.kind === "block");

export const hasMTProto = (frame: MetricsFrame): boolean =>
  frame.mtproto?.enabled === true;

export interface PanelAvailability {
  escalations: boolean;
  blocked: boolean;
  telegram: boolean;
}

const NONE: PanelAvailability = {
  escalations: false,
  blocked: false,
  telegram: false,
};

export function usePanelAvailability(): PanelAvailability {
  const bridgeOn = useTelegramBridgeEnabled();
  const fromFrame = useMetricsFrame(
    (frame) => ({
      escalations: hasEscalations(frame),
      blocked: hasBlocking(frame),
      telegram: hasMTProto(frame),
    }),
    shallowEqual,
  );
  const base = fromFrame ?? NONE;
  return useMemo(
    () => (bridgeOn && !base.telegram ? { ...base, telegram: true } : base),
    [base, bridgeOn],
  );
}

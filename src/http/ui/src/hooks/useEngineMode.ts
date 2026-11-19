import { useMetricsFrame } from "@/stores/useMetrics";

export const useTunEngine = (): boolean =>
  useMetricsFrame((f) => f.engine.mode === "tun") ?? false;

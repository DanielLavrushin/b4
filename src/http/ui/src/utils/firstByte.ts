const SHORTEST_DELAY_MS = 10;
const FALLBACK_DELAY_MS = 30;

const usedDelay = (ms: number) =>
  ms < SHORTEST_DELAY_MS ? FALLBACK_DELAY_MS : ms;

export const firstByteWait = (low: number, high: number, unit: string): string => {
  if (high <= low || high < SHORTEST_DELAY_MS) {
    return `${usedDelay(low)}${unit}+`;
  }
  if (low >= SHORTEST_DELAY_MS) return `${low}-${high}${unit}+`;
  return `${SHORTEST_DELAY_MS}-${Math.max(high, FALLBACK_DELAY_MS)}${unit}+`;
};

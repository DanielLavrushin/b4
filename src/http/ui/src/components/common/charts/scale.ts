export const SCALE_FLOOR = 5;

const NICE_STEPS = [1, 2, 5, 10] as const;

function magnitudeOf(value: number): number {
  let exponent = Math.floor(Math.log10(value));
  if (10 ** (exponent + 1) <= value) exponent++;
  if (10 ** exponent > value) exponent--;
  return 10 ** exponent;
}

function niceAbove(value: number): number {
  const magnitude = magnitudeOf(value);
  for (const step of NICE_STEPS) {
    const candidate = step * magnitude;
    if (candidate >= value * (1 - 1e-9)) return candidate;
  }
  return 10 * magnitude;
}

export function niceCeil(value: number, floor = SCALE_FLOOR): number {
  const minimum = Math.max(1, Math.ceil(floor));
  if (!Number.isFinite(value) || value <= minimum) return minimum;
  return Math.max(minimum, Math.round(niceAbove(value)));
}

export function niceCeilReal(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 1;
  return niceAbove(value);
}

export function yTicks(max: number): number[] {
  if (!(max > 0)) return [0];
  const half = max / 2;
  return Number.isInteger(half) ? [0, half, max] : [0, max];
}

export interface ScaleState {
  max: number;
  key: number;
}

export function nextScale(
  prev: ScaleState | null,
  peak: number,
  key: number,
  floor = SCALE_FLOOR,
): ScaleState {
  const target = niceCeil(peak, floor);
  if (!prev) return { max: target, key };
  if (target > prev.max) return { max: target, key };
  if (key === prev.key) return prev;
  return { max: target, key };
}

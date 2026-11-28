export const ROW_HEIGHT = 41;
const OVERSCAN = 5;

export function visibleRange(
  scrollTop: number,
  viewportHeight: number,
  count: number,
): { start: number; end: number } {
  const size = Math.ceil(viewportHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const start = Math.max(
    0,
    Math.min(Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN, count - size),
  );
  return { start, end: Math.min(count, start + size) };
}

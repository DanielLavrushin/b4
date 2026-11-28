import type { ParsedLog } from "../../hooks/useDomainActions";
import type { AsnLabels } from "../../utils/asn";

export const ROW_HEIGHT = 41;
const OVERSCAN = 5;
export const COLUMN_WIDTHS: (number | string | undefined)[] = [
  110,
  "12%",
  "12%",
  "32%",
  "10%",
  undefined,
];
export const TABLE_MIN_WIDTH = { xs: 1420, lg: 980 };

interface RowInputs {
  log: ParsedLog;
  enrichingIps: Set<string>;
  asnLabels: AsnLabels;
}

export const sameRow = (prev: RowInputs, next: RowInputs): boolean =>
  prev.log.raw === next.log.raw &&
  prev.log.deviceName === next.log.deviceName &&
  prev.enrichingIps === next.enrichingIps &&
  prev.asnLabels === next.asnLabels;

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

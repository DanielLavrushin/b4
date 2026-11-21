const PORT_DIGITS = /^\+?\d+$/;

const portNumber = (text: string): number | null => {
  const value = text.trim();
  if (!PORT_DIGITS.test(value)) return null;
  const port = Number(value);
  return port >= 1 && port <= 65535 ? port : null;
};

const portRange = (entry: string): [number, number] | null => {
  const part = entry.trim().replaceAll(":", "-");
  if (!part.includes("-")) {
    const port = portNumber(part);
    return port === null ? null : [port, port];
  }
  const bounds = part.split("-");
  if (bounds.length !== 2) return null;
  const min = portNumber(bounds[0]);
  const max = portNumber(bounds[1]);
  return min !== null && max !== null && min < max ? [min, max] : null;
};

export const portFilterCovers = (filter: string, port: number): boolean => {
  const ranges = (filter ?? "").split(",").flatMap((entry) => {
    const range = portRange(entry);
    return range ? [range] : [];
  });
  return (
    ranges.length === 0 || ranges.some(([min, max]) => port >= min && port <= max)
  );
};

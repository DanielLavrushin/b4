export const portFilterCovers = (filter: string, port: number): boolean => {
  const parts = (filter ?? "")
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part !== "");
  if (parts.length === 0) return true;
  return parts.some((part) => {
    const bounds = part.split(/[-:]/);
    if (bounds.length === 2) {
      const min = Number(bounds[0]);
      const max = Number(bounds[1]);
      return (
        Number.isInteger(min) &&
        Number.isInteger(max) &&
        port >= min &&
        port <= max
      );
    }
    return Number(part) === port;
  });
};

export type OtherSetsTargets = Map<string, string[]>;

export const findSetOverlaps = (
  input: string,
  otherSetsTargets: OtherSetsTargets | undefined,
  normalize: (value: string) => string = (value) => value,
): string => {
  if (!otherSetsTargets) return "";
  const found: string[] = [];
  for (const raw of input.split(/[\s,|]+/).filter(Boolean)) {
    const item = raw.trim();
    const sets = otherSetsTargets.get(normalize(item));
    if (sets) found.push(`"${item}" is in ${sets.join(", ")}`);
  }
  return found.join("; ");
};

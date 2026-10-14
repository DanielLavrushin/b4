export const setByPath = <T extends object>(
  prev: T,
  field: string,
  value: unknown,
): T => {
  const keys = field.split(".");
  if (keys.length === 1) return { ...prev, [field]: value };
  const next = { ...prev };
  let current = next as Record<string, unknown>;
  for (let i = 0; i < keys.length - 1; i++) {
    current[keys[i]] = { ...(current[keys[i]] as object) };
    current = current[keys[i]] as Record<string, unknown>;
  }
  current[keys.at(-1)!] = value;
  return next;
};

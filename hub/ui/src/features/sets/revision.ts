import type { EntryView } from "@/models/api";

export const revisionOf = (entry: EntryView): string => {
  if (!entry.edited_at) return entry.updated_at;
  return Date.parse(entry.edited_at) > Date.parse(entry.updated_at) ? entry.edited_at : entry.updated_at;
};

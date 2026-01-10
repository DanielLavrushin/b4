import { IconArrowsExchange } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Card } from "@design/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Separator } from "@design/components/ui/separator";
import { cn } from "@design/lib/utils";
import { B4SetConfig } from "@models/config";
import { useMemo } from "react";

interface SetCompareProps {
  open: boolean;
  setA: B4SetConfig | null;
  setB: B4SetConfig | null;
  onClose: () => void;
}

interface DiffItem {
  path: string;
  label: string;
  valueA: unknown;
  valueB: unknown;
  type: "added" | "removed" | "changed" | "same";
}

const IGNORE_KEYS = new Set([
  "id",
  "name",
  "enabled",
  "stats",
  "manual_domains",
  "manual_ips",
  "geosite_domains",
  "geoip_ips",
  "total_domains",
  "total_ips",
  "geosite_category_breakdown",
  "geoip_category_breakdown",
]);

const flattenObject = (
  obj: Record<string, unknown>,
  prefix = ""
): Record<string, unknown> => {
  const result: Record<string, unknown> = {};
  for (const key of Object.keys(obj)) {
    if (IGNORE_KEYS.has(key)) continue;
    const path = prefix ? `${prefix}.${key}` : key;
    const value = obj[key];
    if (value && typeof value === "object" && !Array.isArray(value)) {
      Object.assign(
        result,
        flattenObject(value as Record<string, unknown>, path)
      );
    } else {
      result[path] = value;
    }
  }
  return result;
};

const formatValue = (val: unknown): string => {
  if (val === null || val === undefined) return "—";
  if (Array.isArray(val))
    return val.length === 0 ? "[]" : `[${val.length} items]`;
  if (typeof val === "boolean") return val ? "Yes" : "No";
  if (typeof val === "object") return JSON.stringify(val);
  if (typeof val === "string" || typeof val === "number") return String(val);
  return JSON.stringify(val);
};

const pathToLabel = (path: string): string => {
  return path
    .split(".")
    .map((p) => p.replace(/_/g, " "))
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join(" → ");
};

export const SetCompare = ({ open, setA, setB, onClose }: SetCompareProps) => {
  const diffs = useMemo(() => {
    if (!setA || !setB) return [];

    const flatA = flattenObject(setA as unknown as Record<string, unknown>);
    const flatB = flattenObject(setB as unknown as Record<string, unknown>);
    const allKeys = new Set([...Object.keys(flatA), ...Object.keys(flatB)]);

    const items: DiffItem[] = [];
    for (const path of allKeys) {
      const valA = flatA[path];
      const valB = flatB[path];
      const strA = JSON.stringify(valA);
      const strB = JSON.stringify(valB);

      if (strA === strB) continue; // skip identical

      let type: DiffItem["type"] = "changed";
      if (valA === undefined) type = "added";
      else if (valB === undefined) type = "removed";

      items.push({
        path,
        label: pathToLabel(path),
        valueA: valA,
        valueB: valB,
        type,
      });
    }

    return items.sort((a, b) => a.path.localeCompare(b.path));
  }, [setA, setB]);

  const groupedDiffs = useMemo(() => {
    const groups: Record<string, DiffItem[]> = {};
    for (const diff of diffs) {
      const section = diff.path.split(".")[0];
      if (!groups[section]) groups[section] = [];
      groups[section].push(diff);
    }
    return groups;
  }, [diffs]);

  if (!setA || !setB) return null;

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <IconArrowsExchange />
            </div>
            <div className="flex-1">
              <DialogTitle>Compare Sets</DialogTitle>
              <DialogDescription className="mt-1">
                {setA.name} vs {setB.name}
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>
        <div className="flex-1 overflow-y-auto py-4">
          <div className="mt-4">
            {/* Header */}
            <div className="grid grid-cols-12 gap-4 mb-4">
              <div className="col-span-5">
                <Card className="p-3 bg-accent text-center border border-border">
                  <p className="text-sm font-semibold">{setA.name}</p>
                </Card>
              </div>
              <div className="col-span-2 flex items-center justify-center">
                <IconArrowsExchange className="h-5 w-5 text-muted-foreground" />
              </div>
              <div className="col-span-5">
                <Card className="p-3 bg-accent-secondary text-center border border-border">
                  <p className="text-sm font-semibold">{setB.name}</p>
                </Card>
              </div>
            </div>

            {diffs.length === 0 ? (
              <Card className="p-6 text-center bg-card border border-border">
                <p className="text-muted-foreground">Sets are identical</p>
              </Card>
            ) : (
              <div className="flex flex-col gap-4">
                {Object.entries(groupedDiffs).map(([section, items]) => (
                  <Card
                    key={section}
                    className="overflow-hidden border border-border"
                  >
                    <div className="px-4 py-2 bg-muted">
                      <p className="text-xs font-semibold uppercase text-center">
                        {section}
                      </p>
                    </div>
                    <Separator />
                    <div className="divide-y divide-border">
                      {items.map((diff) => (
                        <div
                          key={diff.path}
                          className="grid grid-cols-12 gap-4 p-3"
                        >
                          <div className="col-span-5">
                            <p
                              className={cn(
                                "text-sm font-mono",
                                diff.type === "removed"
                                  ? "text-destructive/70 line-through"
                                  : "text-foreground"
                              )}
                            >
                              {formatValue(diff.valueA)}
                            </p>
                          </div>
                          <div className="col-span-2 flex items-center justify-center">
                            <Badge
                              className={cn(
                                "text-xs px-1.5 py-0.5 h-6 inline-flex items-center text-muted-foreground",
                                diff.type === "added"
                                  ? "bg-primary/20"
                                  : diff.type === "removed"
                                  ? "bg-destructive/20"
                                  : "bg-secondary/20"
                              )}
                            >
                              {diff.label.split(" → ").pop() || ""}
                            </Badge>
                          </div>
                          <div className="col-span-5">
                            <p
                              className={cn(
                                "text-sm font-mono text-right",
                                diff.type === "added"
                                  ? "text-primary"
                                  : "text-foreground"
                              )}
                            >
                              {formatValue(diff.valueB)}
                            </p>
                          </div>
                        </div>
                      ))}
                    </div>
                  </Card>
                ))}
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};

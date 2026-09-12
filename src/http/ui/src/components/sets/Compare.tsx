import { useMemo } from "react";
import {
  Box,
  Stack,
  Typography,
  Paper,
  Chip,
  Divider,
  Grid,
} from "@mui/material";
import { CompareIcon, AddIcon, ClearIcon, SwapIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { B4SetConfig } from "@models/config";
import { colors } from "@design";

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
  "hub_state",
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
  prefix = "",
): Record<string, unknown> => {
  const result: Record<string, unknown> = {};
  for (const key of Object.keys(obj)) {
    if (IGNORE_KEYS.has(key)) continue;
    const path = prefix ? `${prefix}.${key}` : key;
    const value = obj[key];
    if (value && typeof value === "object" && !Array.isArray(value)) {
      Object.assign(
        result,
        flattenObject(value as Record<string, unknown>, path),
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
    .map((p) => p.replaceAll("_", " "))
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
    <B4Dialog
      open={open}
      onClose={onClose}
      title="Compare Sets"
      subtitle={`${setA.name} vs ${setB.name}`}
      icon={<CompareIcon />}
      maxWidth="lg"
      fullWidth
    >
      <Box sx={{ mt: 2 }}>
        {/* Header */}
        <Grid container spacing={2} sx={{ mb: 2 }}>
          <Grid size={{ xs: 5 }}>
            <Paper
              sx={{
                p: 1.5,
                bgcolor: colors.accent.primary,
                textAlign: "center",
              }}
            >
              <Typography variant="subtitle1" fontWeight={600}>
                {setA.name}
              </Typography>
            </Paper>
          </Grid>
          <Grid
            size={{ xs: 2 }}
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <CompareIcon sx={{ color: colors.text.secondary }} />
          </Grid>
          <Grid size={{ xs: 5 }}>
            <Paper
              sx={{
                p: 1.5,
                bgcolor: colors.accent.secondary,
                textAlign: "center",
              }}
            >
              <Typography variant="subtitle1" fontWeight={600}>
                {setB.name}
              </Typography>
            </Paper>
          </Grid>
        </Grid>

        {diffs.length === 0 ? (
          <Paper
            sx={{ p: 3, textAlign: "center", bgcolor: colors.background.paper }}
          >
            <Typography color="text.secondary">Sets are identical</Typography>
          </Paper>
        ) : (
          <Stack spacing={2}>
            {Object.entries(groupedDiffs).map(([section, items]) => (
              <Paper
                key={section}
                sx={{
                  overflow: "hidden",
                  border: `1px solid ${colors.border.default}`,
                }}
              >
                <Box sx={{ px: 2, py: 1, bgcolor: colors.background.dark }}>
                  <Typography variant="subtitle2" textTransform="uppercase">
                    {section}
                  </Typography>
                </Box>
                <Divider />
                <Stack divider={<Divider />}>
                  {items.map((diff) => (
                    <Grid container key={diff.path} sx={{ p: 1.5 }}>
                      <Grid size={{ xs: 5 }}>
                        <Typography
                          variant="body2"
                          sx={{
                            fontFamily: "monospace",
                            color:
                              diff.type === "removed"
                                ? colors.quaternary
                                : colors.text.primary,
                            textDecoration:
                              diff.type === "added" ? "none" : undefined,
                          }}
                        >
                          {formatValue(diff.valueA)}
                        </Typography>
                      </Grid>
                      <Grid
                        size={{ xs: 2 }}
                        sx={{
                          display: "flex",
                          alignItems: "center",
                          justifyContent: "center",
                        }}
                      >
                        <Chip
                          size="small"
                          icon={
                            diff.type === "added" ? (
                              <AddIcon />
                            ) : diff.type === "removed" ? (
                              <ClearIcon />
                            ) : (
                              <SwapIcon />
                            )
                          }
                          label={diff.label.split(" → ").pop()}
                          sx={{
                            fontSize: "0.7rem",
                            height: 24,
                            bgcolor:
                              diff.type === "added"
                                ? `${colors.tertiary}22`
                                : diff.type === "removed"
                                  ? `${colors.quaternary}22`
                                  : `${colors.secondary}22`,
                            color:
                              diff.type === "added"
                                ? colors.tertiary
                                : diff.type === "removed"
                                  ? colors.quaternary
                                  : colors.secondary,
                          }}
                        />
                      </Grid>
                      <Grid size={{ xs: 5 }}>
                        <Typography
                          variant="body2"
                          sx={{
                            fontFamily: "monospace",
                            color:
                              diff.type === "added"
                                ? colors.tertiary
                                : colors.text.primary,
                          }}
                        >
                          {formatValue(diff.valueB)}
                        </Typography>
                      </Grid>
                    </Grid>
                  ))}
                </Stack>
              </Paper>
            ))}
          </Stack>
        )}
      </Box>
    </B4Dialog>
  );
};

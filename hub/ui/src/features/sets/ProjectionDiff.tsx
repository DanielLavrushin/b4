import { Box, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import type { Projection } from "@/models/api";
import { diffProjections, type DiffKind } from "@/shared/utils/diff";

const kindColor: Record<DiffKind, string> = {
  added: colors.state.success,
  removed: colors.state.error,
  changed: colors.state.warning,
};

const MAX_HEIGHT = "min(480px, 60vh)";

const cellSx = { fontFamily: fonts.mono, fontSize: 12 };

const headSx = { bgcolor: colors.background.dark };

const compactCells = { "& .MuiTableCell-root": { px: 1, "&:first-of-type": { pl: 0 }, "&:last-of-type": { pr: 0 } } };

interface ProjectionDiffProps {
  before: Projection;
  after: Projection;
  beforeVersion: number;
  title?: string;
  emptyText?: string;
  beforeLabel?: string;
  afterLabel?: string;
  compact?: boolean;
}

export function ProjectionDiff({
  before,
  after,
  beforeVersion,
  title,
  emptyText,
  beforeLabel,
  afterLabel,
  compact = false,
}: Readonly<ProjectionDiffProps>) {
  const { t } = useTranslation();
  const lines = useMemo(() => diffProjections(before, after), [before, after]);
  const frameSx = compact
    ? { minWidth: 0 }
    : { border: `1px solid ${colors.border.light}`, borderRadius: 1, bgcolor: colors.background.dark, overflow: "hidden", minWidth: 0 };

  return (
    <Box sx={frameSx}>
      {!compact && (
        <Typography variant="sectionHeader" sx={{ px: 2, pt: 1.5, display: "block" }}>
          {title ?? t("entry.diff.title", { version: beforeVersion })}
        </Typography>
      )}
      {lines.length === 0 ? (
        <Typography variant="body2" sx={{ px: compact ? 0 : 2, py: compact ? 0.5 : 1.5, color: colors.text.secondary }}>
          {emptyText ?? t("entry.diff.none")}
        </Typography>
      ) : (
        <Box sx={{ maxHeight: MAX_HEIGHT, overflow: "auto" }}>
          <Table size="small" stickyHeader sx={{ fontFamily: fonts.mono, ...(compact ? compactCells : {}) }}>
            <TableHead>
              <TableRow>
                <TableCell sx={headSx}>{t("entry.diff.path")}</TableCell>
                <TableCell sx={headSx}>{beforeLabel ?? t("entry.diff.before")}</TableCell>
                <TableCell sx={headSx}>{afterLabel ?? t("entry.diff.after")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {lines.map((line, index) => (
                <TableRow key={`${String(index)}:${line.kind}:${line.path}`}>
                  <TableCell sx={{ ...cellSx, ...(compact ? { overflowWrap: "anywhere" } : { whiteSpace: "nowrap" }) }}>
                    <Box component="span" sx={{ color: kindColor[line.kind], mr: compact ? 0 : 1 }}>
                      {t(`entry.diff.${line.kind}`)}
                    </Box>
                    {compact && " "}
                    {line.path}
                  </TableCell>
                  <TableCell sx={{ ...cellSx, overflowWrap: "anywhere", color: colors.text.secondary }}>{line.before ?? ""}</TableCell>
                  <TableCell sx={{ ...cellSx, overflowWrap: "anywhere" }}>{line.after ?? ""}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Box>
  );
}

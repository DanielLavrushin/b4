import { Box, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import type { Projection } from "@/models/api";
import { diffProjections, type DiffKind } from "@/utils/diff";

const kindColor: Record<DiffKind, string> = {
  added: colors.state.success,
  removed: colors.state.error,
  changed: colors.state.warning,
};

interface ProjectionDiffProps {
  before: Projection;
  after: Projection;
  beforeVersion: number;
}

export function ProjectionDiff({ before, after, beforeVersion }: ProjectionDiffProps) {
  const { t } = useTranslation();
  const lines = useMemo(() => diffProjections(before, after), [before, after]);

  return (
    <Box sx={{ border: `1px solid ${colors.border.light}`, borderRadius: 1, overflowX: "auto" }}>
      <Typography variant="sectionHeader" sx={{ px: 2, pt: 1.5, display: "block" }}>
        {t("entry.diff.title", { version: beforeVersion })}
      </Typography>
      {lines.length === 0 ? (
        <Typography variant="body2" sx={{ px: 2, py: 1.5, color: colors.text.secondary }}>
          {t("entry.diff.none")}
        </Typography>
      ) : (
        <Table size="small" sx={{ fontFamily: fonts.mono }}>
          <TableHead>
            <TableRow>
              <TableCell>{t("entry.diff.path")}</TableCell>
              <TableCell>{t("entry.diff.before")}</TableCell>
              <TableCell>{t("entry.diff.after")}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {lines.map((line) => (
              <TableRow key={line.path + line.kind}>
                <TableCell sx={{ fontFamily: fonts.mono, fontSize: 12, whiteSpace: "nowrap" }}>
                  <Box component="span" sx={{ color: kindColor[line.kind], mr: 1 }}>
                    {t(`entry.diff.${line.kind}`)}
                  </Box>
                  {line.path}
                </TableCell>
                <TableCell sx={{ fontFamily: fonts.mono, fontSize: 12, overflowWrap: "anywhere", color: colors.text.secondary }}>
                  {line.before ?? ""}
                </TableCell>
                <TableCell sx={{ fontFamily: fonts.mono, fontSize: 12, overflowWrap: "anywhere" }}>{line.after ?? ""}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Box>
  );
}

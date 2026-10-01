import { Box, Dialog, DialogContent, DialogTitle, Table, TableBody, TableCell, TableRow } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import type { Hotkey } from "@/shared/hooks/useHotkeys";

export function ShortcutsDialog({ open, onClose, bindings }: Readonly<{ open: boolean; onClose: () => void; bindings: Hotkey[] }>) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{t("shortcuts.title")}</DialogTitle>
      <DialogContent>
        <Table size="small">
          <TableBody>
            {bindings.map((b) => (
              <TableRow key={b.keys}>
                <TableCell sx={{ width: 120 }}>
                  {b.keys.split("|").map((k) => (
                    <Box
                      key={k}
                      component="kbd"
                      sx={{ fontFamily: fonts.mono, fontSize: 12, px: 0.75, py: 0.25, mr: 0.5, border: `1px solid ${colors.border.default}`, borderRadius: 0.5, bgcolor: colors.background.dark }}
                    >
                      {k}
                    </Box>
                  ))}
                </TableCell>
                <TableCell>{t(b.labelKey)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </DialogContent>
    </Dialog>
  );
}

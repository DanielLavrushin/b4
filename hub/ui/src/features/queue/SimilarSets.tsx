import { useState } from "react";
import { Alert, Box, Button, Chip, Collapse, Link, Stack, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { get } from "@/api/client";
import type { EntryView, SimilarView } from "@/models/api";
import { SetRef } from "@/shared/components/SetRef";
import { setRef } from "@/shared/utils/format";
import { errorText } from "@/shared/utils/notices";

const strong = new Set(["same_targets", "same_strategy", "same_title"]);

export const useSimilar = (id: string, version: number) =>
  useQuery({
    queryKey: ["sets", "similar", id, version],
    queryFn: () => get<SimilarView>(`/sets/${encodeURIComponent(id)}/${String(version)}/similar`),
    staleTime: 60_000,
  });

export function SimilarSets({ entry, onReject }: Readonly<{ entry: EntryView; onReject: (e: EntryView, reason?: string) => void }>) {
  const { t } = useTranslation();
  const similar = useSimilar(entry.set_id, entry.version);
  const items = similar.data?.items ?? [];
  const important = items.some((i) => i.relations.some((r) => strong.has(r.code)));
  const [open, setOpen] = useState<boolean | null>(null);
  if (similar.isPending) {
    return (
      <Typography variant="caption" sx={{ color: colors.text.secondary }}>
        {t("similar.checking")}
      </Typography>
    );
  }
  if (similar.isError && items.length === 0) {
    return (
      <Alert
        severity="warning"
        variant="outlined"
        action={
          <Button color="inherit" size="small" onClick={() => void similar.refetch()}>
            {t("app.retry")}
          </Button>
        }
      >
        {t("similar.failed", { message: errorText(t, similar.error) })}
      </Alert>
    );
  }
  if (items.length === 0) return null;
  const shown = open ?? important;
  return (
    <Box sx={{ border: `1px solid ${important ? colors.state.warning : colors.border.light}`, borderRadius: `${radiusPx.sm}px`, p: 1.5 }}>
      <Link component="button" type="button" underline="hover" variant="body2" onClick={() => setOpen(!shown)} sx={{ fontWeight: 600 }}>
        {t("similar.title", { count: items.length })} {shown ? "▴" : "▾"}
      </Link>
      <Collapse in={shown} unmountOnExit>
        <Stack spacing={1} sx={{ mt: 1 }}>
          {items.map((item) => (
            <Box key={setRef(item.set_id, item.version)} sx={{ display: "flex", gap: 1, alignItems: "flex-start", flexWrap: "wrap", borderTop: `1px solid ${colors.border.light}`, pt: 1 }}>
              <Box sx={{ flex: 1, minWidth: 220 }}>
                <SetRef id={item.set_id} version={item.version} title={item.title} status={item.status} />
                <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", mt: 0.5 }}>
                  {item.listed && <Chip size="small" color="success" variant="outlined" label={t("similar.listed")} />}
                  {item.relations.map((r) => (
                    <Chip
                      key={r.code}
                      size="small"
                      variant="outlined"
                      color={strong.has(r.code) ? "warning" : "default"}
                      label={t(`similar.relation.${r.code}`, { count: r.count ?? 0 })}
                    />
                  ))}
                </Box>
                {item.shared.length > 0 && (
                  <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block", mt: 0.5, overflowWrap: "anywhere" }}>
                    {item.shared.map((s) => s.replace(/^[a-z]+:/, "")).join(", ")}
                  </Typography>
                )}
                {item.status_reason && (
                  <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
                    {t("similar.reason", { reason: item.status_reason })}
                  </Typography>
                )}
              </Box>
              <Typography variant="caption" sx={{ color: colors.text.secondary, whiteSpace: "nowrap" }}>
                +{item.votes.works} / -{item.votes.broken}
              </Typography>
              <Button size="small" color="error" onClick={() => onReject(entry, t("similar.duplicateReason", { ref: setRef(item.set_id, item.version), title: item.title }))}>
                {t("similar.rejectAsDuplicate")}
              </Button>
            </Box>
          ))}
          {similar.data?.truncated && (
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("similar.truncated")}
            </Typography>
          )}
        </Stack>
      </Collapse>
    </Box>
  );
}

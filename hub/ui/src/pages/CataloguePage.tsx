import { useState } from "react";
import { Alert, Box, Button, Grid, Stack, TextField, Typography } from "@mui/material";
import InventoryIcon from "@mui/icons-material/Inventory2Outlined";
import BuildIcon from "@mui/icons-material/BuildOutlined";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useCatalogueBuild, useCatalogueEpoch, useCatalogueRevoke, useOverview } from "@/api/hub";
import { useSnackbar } from "@/context/SnackbarProvider";
import { Section } from "@/components/common/Section";
import { Facts } from "@/components/common/Facts";
import { Mono } from "@/components/common/Mono";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { ReasonDialog, type ReasonPrompt } from "@/components/common/ReasonDialog";
import { formatAgo, formatBytes, formatStamp } from "@/utils/format";

export function CataloguePage() {
  const { t } = useTranslation();
  const overview = useOverview();
  const { notify, notifyError } = useSnackbar();
  const build = useCatalogueBuild();
  const epoch = useCatalogueEpoch();
  const revoke = useCatalogueRevoke();
  const [keyId, setKeyId] = useState("");
  const [prompt, setPrompt] = useState<ReasonPrompt | null>(null);

  const run = async (work: () => Promise<{ notice: string }>) => {
    try {
      const result = await work();
      notify(result.notice, "success");
    } catch (err) {
      notifyError(err);
    }
  };

  if (overview.isLoading) return <Loading />;
  if (overview.error) return <ErrorState error={overview.error} />;
  const cat = overview.data?.catalogue;
  if (!cat) return null;
  const busy = build.isPending || epoch.isPending || revoke.isPending;

  return (
    <Grid container spacing={2}>
      <Grid size={{ xs: 12, lg: 6 }}>
        <Section title={t("catalogue.status")} description={t("catalogue.statusDesc")} icon={<InventoryIcon />}>
          {!cat.published ? (
            <Alert severity="warning">{t("overview.notPublished")}</Alert>
          ) : (
            <>
              <Alert severity={cat.dirty ? "info" : "success"} variant="outlined">
                {cat.dirty ? t("overview.dirty") : t("overview.clean")}
              </Alert>
              <Facts
                items={[
                  { label: t("overview.file"), value: <Mono>{cat.file}</Mono> },
                  { label: t("overview.geoSize"), value: formatBytes(cat.size) },
                  { label: t("overview.epoch"), value: `${String(cat.epoch)} / ${String(cat.seq)}` },
                  { label: t("overview.generated"), value: cat.generated_at ? `${formatStamp(cat.generated_at)} (${formatAgo(t, cat.generated_at)})` : "" },
                  { label: t("overview.expires"), value: formatStamp(cat.expires_at) },
                  { label: t("overview.sets"), value: `${String(cat.sets)} / ${String(cat.blobs)}` },
                  { label: t("overview.builtAt"), value: cat.built_at ? formatStamp(cat.built_at) : "" },
                ]}
              />
            </>
          )}
          <Box>
            <Typography variant="metricLabel" sx={{ display: "block", mb: 1 }}>
              {t("catalogue.mirrors")}
            </Typography>
            {cat.mirrors.length === 0 ? (
              <EmptyState text={t("catalogue.noMirrors")} />
            ) : (
              <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
                {cat.mirrors.map((m) => (
                  <li key={m}>
                    <Mono>{m}</Mono>
                  </li>
                ))}
              </Stack>
            )}
          </Box>
          <Box>
            <Typography variant="metricLabel" sx={{ display: "block", mb: 1 }}>
              {t("catalogue.revoked")}
            </Typography>
            {cat.revoked_keys.length === 0 ? (
              <EmptyState text={t("catalogue.noRevoked")} />
            ) : (
              <Stack component="ul" sx={{ m: 0, pl: "1.1em" }} spacing={0.25}>
                {cat.revoked_keys.map((k) => (
                  <li key={k}>
                    <Mono>{k}</Mono>
                  </li>
                ))}
              </Stack>
            )}
          </Box>
        </Section>
      </Grid>
      <Grid size={{ xs: 12, lg: 6 }}>
        <Section title={t("catalogue.actions")} description={t("catalogue.actionsDesc")} icon={<BuildIcon />}>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            <Typography variant="sectionHeader">{t("catalogue.build")}</Typography>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("catalogue.buildText")}
            </Typography>
            <Box>
              <Button variant="contained" disabled={busy} onClick={() => void run(() => build.mutateAsync())}>
                {t("catalogue.build")}
              </Button>
            </Box>
          </Box>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            <Typography variant="sectionHeader">{t("catalogue.epoch")}</Typography>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("catalogue.epochText")}
            </Typography>
            <Box>
              <Button
                variant="outlined"
                color="warning"
                disabled={busy}
                onClick={() =>
                  setPrompt({
                    title: t("catalogue.epoch"),
                    text: t("catalogue.epochConfirm"),
                    confirmLabel: t("app.confirm"),
                    reason: "none",
                    destructive: true,
                    onConfirm: () => run(() => epoch.mutateAsync()),
                  })
                }
              >
                {t("catalogue.epoch")}
              </Button>
            </Box>
          </Box>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            <Typography variant="sectionHeader">{t("catalogue.revoke")}</Typography>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("catalogue.revokeText")}
            </Typography>
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
              <TextField size="small" fullWidth placeholder={t("catalogue.revokePlaceholder")} value={keyId} onChange={(e) => setKeyId(e.target.value)} />
              <Button
                variant="outlined"
                color="error"
                disabled={busy || keyId.trim() === ""}
                sx={{ whiteSpace: "nowrap" }}
                onClick={() =>
                  setPrompt({
                    title: t("catalogue.revoke"),
                    text: t("catalogue.revokeConfirm", { key: keyId.trim() }),
                    confirmLabel: t("app.confirm"),
                    reason: "none",
                    destructive: true,
                    onConfirm: async () => {
                      await run(() => revoke.mutateAsync(keyId.trim()));
                      setKeyId("");
                    },
                  })
                }
              >
                {t("catalogue.revoke")}
              </Button>
            </Stack>
          </Box>
        </Section>
      </Grid>
      <ReasonDialog prompt={prompt} onClose={() => setPrompt(null)} />
    </Grid>
  );
}

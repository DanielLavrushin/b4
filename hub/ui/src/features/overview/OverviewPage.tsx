import { Alert, Box, Button, Chip, Grid, Link } from "@mui/material";
import InventoryIcon from "@mui/icons-material/Inventory2Outlined";
import HubIcon from "@mui/icons-material/HubOutlined";
import GavelIcon from "@mui/icons-material/GavelOutlined";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useHealth, useOverview } from "@/features/overview/api";
import { HealthCard } from "./HealthCard";
import { Section } from "@/shared/components/Section";
import { StatTile } from "@/shared/components/StatTile";
import { Facts } from "@/shared/components/Facts";
import { Mono } from "@/shared/components/Mono";
import { ErrorState, Loading } from "@/shared/components/States";
import { formatAgo, formatStamp } from "@/shared/utils/format";

export function OverviewPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const overview = useOverview();
  const health = useHealth();

  const data = overview.data;
  if (!data) {
    return overview.error ? <ErrorState error={overview.error} onRetry={() => void overview.refetch()} /> : <Loading />;
  }
  const c = data.counts;
  const cat = data.catalogue;
  const go = (path: string) => () => void navigate(path);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      {health.data && <HealthCard health={health.data} />}
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, sm: 6, lg: 3 }}>
          <StatTile label={t("overview.pending")} value={c.pending} hint={t("overview.pendingHint")} accent={colors.state.warning} onClick={go("/queue")} />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, lg: 3 }}>
          <StatTile label={t("overview.listed")} value={c.listed} hint={t("overview.listedHint")} accent={colors.state.success} onClick={go("/sets")} />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, lg: 3 }}>
          <StatTile label={t("overview.keys")} value={c.keys} hint={t("overview.keysHint", { banned: c.banned })} accent={colors.primaryLight} onClick={go("/keys")} />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, lg: 3 }}>
          <StatTile
            label={t("overview.mirrors")}
            value={c.mirrors_approved}
            hint={t("overview.mirrorsHint", { announced: cat.announced_mirrors.length, pending: c.mirrors_pending })}
            accent={colors.state.info}
            onClick={go("/mirrors")}
          />
        </Grid>
      </Grid>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section
            title={t("overview.catalogue")}
            description={t("overview.catalogueDesc")}
            icon={<InventoryIcon />}
            action={
              <Button size="small" variant="outlined" onClick={go("/catalogue")}>
                {t("nav.catalogue")}
              </Button>
            }
          >
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
                    { label: t("overview.epoch"), value: `${String(cat.epoch)} / ${String(cat.seq)}` },
                    { label: t("overview.generated"), value: cat.generated_at ? `${formatStamp(cat.generated_at)} (${formatAgo(t, cat.generated_at)})` : "" },
                    { label: t("overview.expires"), value: formatStamp(cat.expires_at) },
                    { label: t("overview.sets"), value: `${String(cat.sets)} / ${String(cat.blobs)}` },
                    {
                      label: t("overview.mirrorsAnnounced"),
                      value: cat.hub_listed ? t("overview.plusHub", { count: cat.announced_mirrors.length }) : String(cat.announced_mirrors.length),
                    },
                    { label: t("overview.builtAt"), value: cat.built_at ? formatAgo(t, cat.built_at) : "" },
                  ]}
                />
              </>
            )}
          </Section>
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section title={t("overview.hub")} description={t("overview.hubDesc")} icon={<HubIcon />}>
            <Facts
              items={[
                {
                  label: t("overview.version"),
                  value: (
                    <>
                      {data.version}
                      {health.data?.build_info.dev && <Chip size="small" color="warning" variant="outlined" label={t("overview.devBuild")} sx={{ ml: 1 }} />}
                    </>
                  ),
                },
                {
                  label: t("overview.source"),
                  value: (
                    <>
                      {data.source ?? ""}
                      {health.data?.build_info.dirty_source && <Chip size="small" color="warning" variant="outlined" label={t("overview.dirtySource")} sx={{ ml: 1 }} />}
                    </>
                  ),
                },
                { label: t("overview.keyId"), value: data.key_id, mono: true },
                {
                  label: t("overview.publicUrl"),
                  value: data.public_url ? (
                    <Link href={data.public_url} rel="noreferrer" underline="hover">
                      {data.public_url}
                    </Link>
                  ) : (
                    ""
                  ),
                },
                { label: t("overview.revoked"), value: String(cat.revoked_keys.length) },
              ]}
            />
          </Section>
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section title={t("overview.moderation")} description={t("overview.moderationDesc")} icon={<GavelIcon />}>
            <Facts
              items={[
                { label: t("overview.listed"), value: String(c.listed) },
                { label: t("overview.superseded"), value: String(c.superseded) },
                { label: t("overview.withheld"), value: String(c.withheld) },
                { label: t("overview.hidden"), value: String(c.hidden) },
                { label: t("overview.rejected"), value: String(c.rejected) },
                { label: t("overview.votes"), value: String(c.votes) },
                { label: t("overview.reports"), value: String(c.reports) },
                { label: t("overview.reportsOpen"), value: String(c.reports_open) },
              ]}
            />
          </Section>
        </Grid>
      </Grid>
    </Box>
  );
}

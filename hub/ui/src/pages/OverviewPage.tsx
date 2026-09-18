import { Alert, Box, Button, Grid, Link, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import InventoryIcon from "@mui/icons-material/Inventory2Outlined";
import HubIcon from "@mui/icons-material/HubOutlined";
import PublicIcon from "@mui/icons-material/PublicOutlined";
import GavelIcon from "@mui/icons-material/GavelOutlined";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useOverview } from "@/api/hub";
import { Section } from "@/components/common/Section";
import { StatTile } from "@/components/common/StatTile";
import { Facts } from "@/components/common/Facts";
import { Mono } from "@/components/common/Mono";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { formatAgo, formatBytes, formatStamp } from "@/utils/format";

export function OverviewPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const overview = useOverview();

  if (overview.isLoading) return <Loading />;
  if (overview.error) return <ErrorState error={overview.error} />;
  const data = overview.data;
  if (!data) return null;
  const c = data.counts;
  const cat = data.catalogue;
  const go = (path: string) => () => void navigate(path);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
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
            value={c.mirrors_approved + c.mirrors_pending + c.mirrors_rejected}
            hint={t("overview.mirrorsHint", { approved: c.mirrors_approved, pending: c.mirrors_pending })}
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
                    { label: t("overview.mirrorsListed"), value: String(cat.mirrors.length) },
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
                { label: t("overview.version"), value: data.version },
                { label: t("overview.source"), value: data.source ?? "" },
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
                { label: t("overview.hidden"), value: String(c.hidden) },
                { label: t("overview.rejected"), value: String(c.rejected) },
                { label: t("overview.votes"), value: String(c.votes) },
                { label: t("overview.reports"), value: String(c.reports) },
              ]}
            />
          </Section>
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section title={t("overview.geo")} description={t("overview.geoDesc")} icon={<PublicIcon />}>
            {data.geo.length === 0 ? (
              <EmptyState text={t("overview.geoNone")} />
            ) : (
              <Box sx={{ overflowX: "auto" }}>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>{t("overview.geoFile")}</TableCell>
                      <TableCell>{t("overview.geoFetched")}</TableCell>
                      <TableCell>{t("overview.geoSize")}</TableCell>
                      <TableCell>{t("overview.geoSource")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {data.geo.map((g) => (
                      <TableRow key={g.name}>
                        <TableCell><Mono>{g.name}</Mono></TableCell>
                        <TableCell sx={{ whiteSpace: "nowrap" }}>
                          {g.error ? (
                            <Typography component="span" variant="body2" sx={{ color: colors.state.error }}>
                              {t("overview.geoError")}: {g.error}
                            </Typography>
                          ) : (
                            <span title={formatStamp(g.fetched_at)}>{formatAgo(t, g.fetched_at)}</span>
                          )}
                        </TableCell>
                        <TableCell>{formatBytes(g.size)}</TableCell>
                        <TableCell sx={{ overflowWrap: "anywhere" }}>
                          <Link href={g.url} rel="noreferrer" underline="hover" variant="caption">
                            {g.url}
                          </Link>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            )}
          </Section>
        </Grid>
      </Grid>
    </Box>
  );
}

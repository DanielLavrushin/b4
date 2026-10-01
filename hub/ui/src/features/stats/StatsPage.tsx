import {
  Box,
  Grid,
  LinearProgress,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@mui/material";
import InsightsIcon from "@mui/icons-material/InsightsOutlined";
import GavelIcon from "@mui/icons-material/GavelOutlined";
import StarIcon from "@mui/icons-material/StarOutline";
import PublicIcon from "@mui/icons-material/PublicOutlined";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { MixView, StatsDayView } from "@/models/api";
import { Section } from "@/shared/components/Section";
import { StatTile } from "@/shared/components/StatTile";
import { SetRef } from "@/shared/components/SetRef";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { DailyBars, type Series } from "@/shared/charts/DailyBars";
import { Histogram } from "@/shared/charts/Histogram";
import { seriesColors } from "@/shared/charts/palette";
import { useStats } from "./api";

const ranges = [7, 30, 90, 365];

type DayKey = Exclude<keyof StatsDayView, "day">;

const dayValues = (daily: StatsDayView[], keys: DayKey[]) => {
  const out: Record<string, Record<string, number>> = {};
  daily.forEach((d) => {
    out[d.day] = Object.fromEntries(keys.map((k) => [k, d[k]]));
  });
  return out;
};

function Activity({
  title,
  daily,
  series,
}: Readonly<{
  title: string;
  daily: StatsDayView[];
  series: (Series & { key: DayKey })[];
}>) {
  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 600, mb: 1 }}>
        {title}
      </Typography>
      <DailyBars
        label={title}
        days={daily.map((d) => d.day)}
        values={dayValues(
          daily,
          series.map((s) => s.key),
        )}
        series={series}
      />
    </Box>
  );
}

function Mix({
  title,
  rows,
  label,
  empty,
  keysOnly = false,
}: Readonly<{
  title: string;
  rows: MixView[];
  label: (m: MixView) => string;
  empty: string;
  keysOnly?: boolean;
}>) {
  const { t } = useTranslation();
  const measure = (r: MixView) => (keysOnly ? r.keys : r.votes);
  const max = Math.max(1, ...rows.map(measure));
  return (
    <Section title={title} dense>
      {rows.length === 0 ? (
        <EmptyState text={empty} />
      ) : (
        <Box sx={{ overflowX: "auto" }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell />
                {!keysOnly && (
                  <TableCell align="right">{t("stats.votes")}</TableCell>
                )}
                <TableCell align="right">{t("stats.keys")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.key}>
                  <TableCell sx={{ width: "60%" }}>
                    <Typography
                      variant="body2"
                      sx={{ overflowWrap: "anywhere" }}
                    >
                      {label(r)}
                    </Typography>
                    <LinearProgress
                      variant="determinate"
                      value={(measure(r) / max) * 100}
                      sx={{
                        height: 3,
                        borderRadius: 1,
                        mt: 0.5,
                        bgcolor: colors.accent.primaryStrong,
                      }}
                    />
                  </TableCell>
                  {!keysOnly && <TableCell align="right">{r.votes}</TableCell>}
                  <TableCell align="right">{r.keys}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Section>
  );
}

export function StatsPage() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const requested = Number(params.get("days") ?? "30");
  const days = ranges.includes(requested) ? requested : 30;
  const stats = useStats(days);
  const data = stats.data;
  if (!data) {
    return stats.error ? (
      <ErrorState error={stats.error} onRetry={() => void stats.refetch()} />
    ) : (
      <Loading />
    );
  }
  const tot = data.totals;
  const sc = data.scores;
  const unknown = t("stats.unknown");

  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        gap: 3,
        opacity: stats.isPlaceholderData ? 0.6 : 1,
      }}
    >
      <Box
        sx={{ display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}
      >
        <Typography
          variant="body2"
          sx={{ flex: 1, color: colors.text.secondary }}
        >
          {t("stats.range", { from: data.from, to: data.to })}
        </Typography>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={days}
          onChange={(_e, v: number | null) => {
            if (v !== null)
              setParams(v === 30 ? {} : { days: String(v) }, { replace: true });
          }}
        >
          {ranges.map((r) => (
            <ToggleButton
              key={r}
              value={r}
              sx={{ textTransform: "none", px: 1.5 }}
            >
              {t("stats.days", { count: r })}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
      </Box>

      <Grid container spacing={2}>
        <Grid size={{ xs: 6, md: 3 }}>
          <StatTile
            label={t("stats.tiles.shares")}
            value={tot.shares}
            hint={t("stats.tiles.sharesHint", { duplicates: tot.duplicates })}
            accent={seriesColors.share}
          />
        </Grid>
        <Grid size={{ xs: 6, md: 3 }}>
          <StatTile
            label={t("stats.tiles.votes")}
            value={tot.works + tot.broken}
            hint={t("stats.tiles.votesHint", {
              works: tot.works,
              broken: tot.broken,
            })}
            accent={seriesColors.vote}
          />
        </Grid>
        <Grid size={{ xs: 6, md: 3 }}>
          <StatTile
            label={t("stats.tiles.keys")}
            value={tot.new_keys}
            hint={t("stats.tiles.keysHint")}
            accent={seriesColors.keys}
          />
        </Grid>
        <Grid size={{ xs: 6, md: 3 }}>
          <StatTile
            label={t("stats.tiles.reports")}
            value={tot.reports}
            hint={t("stats.tiles.reportsHint", { hidden: tot.auto_hidden })}
            accent={seriesColors.report}
          />
        </Grid>
      </Grid>

      <Section
        title={t("stats.activity")}
        description={t("stats.activityDesc")}
        icon={<InsightsIcon />}
      >
        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          <Activity
            title={t("stats.submissions")}
            daily={data.daily}
            series={[
              {
                key: "shares",
                label: t("stats.series.shares"),
                color: seriesColors.share,
              },
              {
                key: "duplicates",
                label: t("stats.series.duplicates"),
                color: colors.text.disabled,
              },
              {
                key: "new_keys",
                label: t("stats.series.new_keys"),
                color: seriesColors.keys,
              },
            ]}
          />
          <Activity
            title={t("stats.feedback")}
            daily={data.daily}
            series={[
              {
                key: "works",
                label: t("stats.series.works"),
                color: seriesColors.works,
              },
              {
                key: "broken",
                label: t("stats.series.broken"),
                color: seriesColors.broken,
              },
              {
                key: "reports",
                label: t("stats.series.reports"),
                color: seriesColors.report,
              },
            ]}
          />
        </Box>
      </Section>

      <Section
        title={t("stats.moderation")}
        description={t("stats.moderationDesc")}
        icon={<GavelIcon />}
      >
        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          <Activity
            title={t("stats.decisions")}
            daily={data.daily}
            series={[
              {
                key: "approved",
                label: t("stats.series.approved"),
                color: colors.state.success,
              },
              {
                key: "rejected",
                label: t("stats.series.rejected"),
                color: colors.state.error,
              },
              {
                key: "hidden",
                label: t("stats.series.hidden"),
                color: colors.text.disabled,
              },
              {
                key: "auto_hidden",
                label: t("stats.series.auto_hidden"),
                color: colors.state.warning,
              },
              {
                key: "withdrawn",
                label: t("stats.series.withdrawn"),
                color: colors.primaryLight,
              },
            ]}
          />
          <Activity
            title={t("stats.buildsTitle", {
              builds: tot.builds,
              failed: tot.build_failed,
            })}
            daily={data.daily}
            series={[
              {
                key: "builds",
                label: t("stats.series.builds"),
                color: seriesColors.builds,
              },
              {
                key: "build_failed",
                label: t("stats.series.build_failed"),
                color: seriesColors.failures,
              },
              {
                key: "mirrors",
                label: t("stats.series.mirrors"),
                color: seriesColors.mirror,
              },
            ]}
          />
        </Box>
      </Section>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section
            title={t("stats.scores")}
            description={t("stats.scoresDesc")}
            icon={<StarIcon />}
          >
            {sc.listed === 0 ? (
              <EmptyState text={t("stats.noCatalogue")} />
            ) : (
              <>
                <Histogram
                  bins={sc.bins.map((b) => ({
                    label: b.lo.toFixed(1),
                    values: { sets: b.sets, low_n: b.low_n },
                    tip: t("stats.binTip", {
                      lo: b.lo.toFixed(1),
                      hi: b.hi.toFixed(1),
                      sets: b.sets,
                      low: b.low_n,
                    }),
                  }))}
                  series={[
                    {
                      key: "sets",
                      label: t("stats.series.rated"),
                      color: colors.state.success,
                    },
                    {
                      key: "low_n",
                      label: t("stats.series.low_n"),
                      color: colors.text.disabled,
                    },
                  ]}
                />
                <Typography variant="body2" sx={{ mt: 2 }}>
                  {t("stats.scoreLine", {
                    listed: sc.listed,
                    rated: sc.rated,
                    median: sc.rated > 0 ? sc.median.toFixed(2) : "-",
                  })}
                </Typography>
                <Typography
                  variant="caption"
                  sx={{ color: colors.text.secondary }}
                >
                  {t("stats.demotedLine", {
                    low: sc.low_score,
                    stale: sc.stale,
                  })}
                </Typography>
              </>
            )}
          </Section>
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Section
            title={t("stats.topSets")}
            description={t("stats.topSetsDesc")}
            icon={<StarIcon />}
          >
            {data.top_sets.length === 0 ? (
              <EmptyState text={t("stats.noVotes")} />
            ) : (
              <Box sx={{ overflowX: "auto" }}>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>{t("stats.set")}</TableCell>
                      <TableCell align="right">
                        {t("stats.series.works")}
                      </TableCell>
                      <TableCell align="right">
                        {t("stats.series.broken")}
                      </TableCell>
                      <TableCell align="right">{t("stats.keys")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {data.top_sets.map((s) => (
                      <TableRow key={s.set_id}>
                        <TableCell>
                          <SetRef
                            id={s.set_id}
                            title={s.title || s.set_id.slice(0, 10)}
                            status={s.status}
                          />
                        </TableCell>
                        <TableCell align="right">{s.works}</TableCell>
                        <TableCell align="right">{s.broken}</TableCell>
                        <TableCell align="right">{s.keys}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            )}
          </Section>
        </Grid>
      </Grid>

      <Section
        title={t("stats.origin")}
        description={t("stats.originDesc", {
          votes: data.coverage.votes,
          unverified: data.coverage.unverified,
          devices: data.coverage.devices,
        })}
        icon={<PublicIcon />}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 4 }}>
            <Mix
              title={t("stats.countries")}
              rows={data.countries}
              empty={t("stats.noOrigin")}
              label={(m) => m.key || unknown}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <Mix
              title={t("stats.asns")}
              rows={data.asns}
              empty={t("stats.noOrigin")}
              label={(m) =>
                m.key
                  ? `AS${m.key}${m.name ? ` ${m.name}` : ""}${m.country ? ` (${m.country})` : ""}`
                  : unknown
              }
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <Mix
              title={t("stats.clients")}
              rows={data.clients}
              empty={t("stats.noVotes")}
              keysOnly
              label={(m) =>
                m.key ? `b4 ${m.key}${m.name ? ` ${m.name}` : ""}` : unknown
              }
            />
          </Grid>
        </Grid>
        <Typography
          variant="caption"
          sx={{ display: "block", mt: 1.5, color: colors.text.disabled }}
        >
          {t("stats.testExcluded")}
        </Typography>
      </Section>
    </Box>
  );
}

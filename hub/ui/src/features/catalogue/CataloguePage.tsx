import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Chip,
  FormControlLabel,
  Grid,
  Stack,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import InventoryIcon from "@mui/icons-material/Inventory2Outlined";
import BuildIcon from "@mui/icons-material/BuildOutlined";
import HistoryIcon from "@mui/icons-material/HistoryOutlined";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import { useOverview } from "@/features/overview/api";
import type { ActionResult, BuildRunView, CatalogueView } from "@/models/api";
import { Section } from "@/shared/components/Section";
import { Facts } from "@/shared/components/Facts";
import { Mono } from "@/shared/components/Mono";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { QueryView } from "@/shared/components/QueryView";
import { ReasonDialog, type ReasonPrompt } from "@/shared/components/ReasonDialog";
import { formatAgo, formatBytes, formatStamp } from "@/shared/utils/format";
import { useBuilds, useCatalogueBuild, useCatalogueEpoch, useCatalogueRevoke } from "./api";

function changeSummary(t: (key: string, params?: Record<string, unknown>) => string, b: BuildRunView): string {
  const c = b.changes;
  const parts: string[] = [];
  if (c.added?.length) parts.push(t("catalogue.history.added", { count: c.added.length }));
  if (c.removed?.length) parts.push(t("catalogue.history.removed", { count: c.removed.length }));
  if (c.updated?.length) parts.push(t("catalogue.history.updated", { count: c.updated.length }));
  if (c.edited?.length) parts.push(t("catalogue.history.edited", { count: c.edited.length }));
  if (c.rescored) parts.push(t("catalogue.history.rescored", { count: c.rescored }));
  if (c.mirrors_added?.length || c.mirrors_removed?.length) parts.push(t("catalogue.history.mirrors"));
  if (c.revoked_added?.length) parts.push(t("catalogue.history.revoked", { count: c.revoked_added.length }));
  return parts.length ? parts.join(", ") : t("catalogue.history.unchanged");
}

function BuildDetails({ build }: { build: BuildRunView }) {
  const { t } = useTranslation();
  const c = build.changes;
  const lines: string[] = [];
  c.added?.forEach((s) => lines.push(`+ ${s.title} (${s.set_id}/${String(s.version)})`));
  c.removed?.forEach((s) => lines.push(`- ${s.title} (${s.set_id}/${String(s.version)})`));
  c.updated?.forEach((s) => lines.push(`~ ${s.title}: v${String(s.from)} -> v${String(s.to)}`));
  c.edited?.forEach((s) => lines.push(`~ ${s.title} ${t("catalogue.history.textOnly")}`));
  c.mirrors_added?.forEach((m) => lines.push(`+ ${m}`));
  c.mirrors_removed?.forEach((m) => lines.push(`- ${m}`));
  c.revoked_added?.forEach((k) => lines.push(`${t("catalogue.history.revokedKey")} ${k}`));
  if (lines.length === 0) return null;
  return (
    <Box component="ul" sx={{ m: 0, mt: 0.5, pl: "1.1em", color: colors.text.secondary, fontSize: 12 }}>
      {lines.slice(0, 20).map((line) => (
        <li key={line}>{line}</li>
      ))}
      {lines.length > 20 && <li>{t("app.andMore", { count: lines.length - 20 })}</li>}
    </Box>
  );
}

function BuildHistory() {
  const { t } = useTranslation();
  const [only, setOnly] = useState<"" | "changes" | "failed">("changes");
  const builds = useBuilds(only);
  const items = builds.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Section title={t("catalogue.history.title")} description={t("catalogue.history.desc")} icon={<HistoryIcon />}>
      <Tabs value={only} onChange={(_e, v: "" | "changes" | "failed") => setOnly(v)}>
        <Tab value="changes" label={t("catalogue.history.onlyChanges")} />
        <Tab value="failed" label={t("catalogue.history.onlyFailed")} />
        <Tab value="" label={t("catalogue.history.all")} />
      </Tabs>
      {builds.isLoading && <Loading />}
      {builds.error && <ErrorState error={builds.error} onRetry={() => void builds.refetch()} />}
      {builds.data && items.length === 0 && <EmptyState text={t("catalogue.history.empty")} />}
      {items.length > 0 && (
        <Box sx={{ overflowX: "auto" }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t("catalogue.history.when")}</TableCell>
                <TableCell>{t("catalogue.history.trigger")}</TableCell>
                <TableCell>{t("catalogue.history.result")}</TableCell>
                <TableCell>{t("catalogue.history.changes")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((b) => (
                <TableRow key={b.id}>
                  <TableCell sx={{ whiteSpace: "nowrap", verticalAlign: "top" }} title={formatStamp(b.started_at)}>
                    {formatAgo(t, b.started_at)}
                  </TableCell>
                  <TableCell sx={{ verticalAlign: "top" }}>{t(`build.trigger.${b.trigger}`, { defaultValue: b.trigger })}</TableCell>
                  <TableCell sx={{ verticalAlign: "top", whiteSpace: "nowrap" }}>
                    {b.ok ? (
                      <Typography variant="body2">
                        {t("catalogue.history.ok", { seq: b.seq ?? 0, sets: b.sets, ms: b.duration_ms })}
                      </Typography>
                    ) : (
                      <Chip size="small" color="error" variant="outlined" label={t("build.failed")} />
                    )}
                  </TableCell>
                  <TableCell sx={{ verticalAlign: "top" }}>
                    {b.ok ? (
                      <>
                        <Typography variant="body2">{changeSummary(t, b)}</Typography>
                        <BuildDetails build={b} />
                      </>
                    ) : (
                      <Typography variant="body2" sx={{ color: colors.state.error, overflowWrap: "anywhere" }}>
                        {b.error}
                      </Typography>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
      {builds.hasNextPage && (
        <Box>
          <Button size="small" disabled={builds.isFetchingNextPage} onClick={() => void builds.fetchNextPage()}>
            {t("app.loadMore")}
          </Button>
        </Box>
      )}
    </Section>
  );
}

function Revoke({ cat, busy, run }: { cat: CatalogueView; busy: boolean; run: (work: () => Promise<ActionResult>) => Promise<void> }) {
  const { t } = useTranslation();
  const revoke = useCatalogueRevoke();
  const [keyId, setKeyId] = useState("");
  const [allowBuiltin, setAllowBuiltin] = useState(false);
  const [prompt, setPrompt] = useState<ReasonPrompt | null>(null);
  const value = keyId.trim();
  const builtin = cat.builtin_keys.includes(value);
  const own = value === cat.signing_key;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="sectionHeader">{t("catalogue.revoke")}</Typography>
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t("catalogue.revokeText")}
      </Typography>
      <Alert severity="warning" variant="outlined">
        {t("catalogue.revokeForever")}
      </Alert>
      <Typography variant="caption" sx={{ color: colors.text.secondary }}>
        {t("catalogue.thisHubKey")} <Mono>{cat.signing_key}</Mono>
      </Typography>
      <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
        <TextField size="small" fullWidth placeholder={t("catalogue.revokePlaceholder")} value={keyId} onChange={(e) => setKeyId(e.target.value)} />
        <Button
          variant="outlined"
          color="error"
          disabled={busy || revoke.isPending || value === "" || own || (builtin && !allowBuiltin)}
          sx={{ whiteSpace: "nowrap" }}
          onClick={() =>
            setPrompt({
              title: t("catalogue.revoke"),
              text: t("catalogue.revokeConfirm", { key: value }),
              confirmLabel: t("catalogue.revokeButton"),
              reason: "none",
              destructive: true,
              confirmText: value,
              onConfirm: async () => {
                await run(() => revoke.mutateAsync({ keyId: value, confirm: value, allowBuiltin }));
                setKeyId("");
                setAllowBuiltin(false);
              },
            })
          }
        >
          {t("catalogue.revokeButton")}
        </Button>
      </Stack>
      {own && <Alert severity="error">{t("errors.own_key")}</Alert>}
      {builtin && (
        <>
          <Alert severity="error">{t("catalogue.revokeBuiltin")}</Alert>
          <FormControlLabel
            control={<Checkbox checked={allowBuiltin} onChange={(e) => setAllowBuiltin(e.target.checked)} />}
            label={t("catalogue.revokeBuiltinAllow")}
          />
        </>
      )}
      <ReasonDialog prompt={prompt} onClose={() => setPrompt(null)} />
    </Box>
  );
}

export function CataloguePage() {
  const { t } = useTranslation();
  const overview = useOverview();
  const { notifyResult, notifyError } = useSnackbar();
  const build = useCatalogueBuild();
  const epoch = useCatalogueEpoch();
  const [prompt, setPrompt] = useState<ReasonPrompt | null>(null);

  const confirmed = async (work: () => Promise<ActionResult>) => {
    try {
      notifyResult(await work());
    } catch (err) {
      notifyError(err);
      throw err;
    }
  };
  const run = (work: () => Promise<ActionResult>) => confirmed(work).catch(() => undefined);

  return (
    <QueryView query={overview}>
      {(data) => {
        const cat = data.catalogue;
        const busy = build.isPending || epoch.isPending;
        const lastError = data.build.last_error;
        return (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, lg: 6 }}>
              <Section title={t("catalogue.status")} description={t("catalogue.statusDesc")} icon={<InventoryIcon />}>
                {lastError && (
                  <Alert severity="error">
                    {t("build.failedHint", { when: formatStamp(lastError.finished_at), message: lastError.error ?? "" })}
                  </Alert>
                )}
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
                        { label: t("catalogue.size"), value: formatBytes(cat.size) },
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
                          {!cat.announced_mirrors.includes(m) && (
                            <Typography component="span" variant="caption" sx={{ color: colors.text.secondary, ml: 1 }}>
                              {t("catalogue.thisHub")}
                            </Typography>
                          )}
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
                          onConfirm: () => confirmed(() => epoch.mutateAsync()),
                        })
                      }
                    >
                      {t("catalogue.epoch")}
                    </Button>
                  </Box>
                </Box>
                <Revoke cat={cat} busy={busy} run={confirmed} />
              </Section>
            </Grid>
            <Grid size={12}>
              <BuildHistory />
            </Grid>
            <ReasonDialog prompt={prompt} onClose={() => setPrompt(null)} />
          </Grid>
        );
      }}
    </QueryView>
  );
}

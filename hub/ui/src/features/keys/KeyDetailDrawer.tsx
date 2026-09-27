import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Chip,
  Drawer,
  IconButton,
  Link,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { HistoryList } from "@/features/audit/HistoryList";
import type { KeyDetailView, KeyTag } from "@/models/api";
import { QueryView } from "@/shared/components/QueryView";
import { Facts } from "@/shared/components/Facts";
import { Mono } from "@/shared/components/Mono";
import { SetRef } from "@/shared/components/SetRef";
import { EmptyState } from "@/shared/components/States";
import { DailyBars, lastDays } from "@/shared/charts/DailyBars";
import { seriesColors } from "@/shared/charts/palette";
import { formatAgo, formatN, formatStamp } from "@/shared/utils/format";
import { AppliedWeight } from "@/features/feedback/AppliedWeight";
import { KeyStatus } from "./KeysPage";
import { useKeyDetail, useKeyProfile } from "./api";

function Section({
  title,
  children,
}: Readonly<{ title: string; children: React.ReactNode }>) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="sectionHeader">{title}</Typography>
      {children}
    </Box>
  );
}

function Profile({ data }: Readonly<{ data: KeyDetailView }>) {
  const { t } = useTranslation();
  const save = useKeyProfile();
  const { notifyResult, notifyError } = useSnackbar();
  const [name, setName] = useState(data.key.name ?? "");
  const [note, setNote] = useState(data.key.note ?? "");
  const [tag, setTag] = useState<KeyTag | "">(data.key.tag ?? "");
  useEffect(() => {
    setName(data.key.name ?? "");
    setNote(data.key.note ?? "");
    setTag(data.key.tag ?? "");
  }, [data.key.key_hmac, data.key.name, data.key.note, data.key.tag]);
  const dirty =
    name !== (data.key.name ?? "") ||
    note !== (data.key.note ?? "") ||
    tag !== (data.key.tag ?? "");
  const crossesTest = (tag === "test") !== (data.key.tag === "test");
  const submit = async () => {
    try {
      notifyResult(
        await save.mutateAsync({
          key: data.key.key_hmac,
          profile: { name, note, tag },
        }),
      );
    } catch (err) {
      notifyError(err);
    }
  };
  return (
    <Section title={t("keys.profile.title")}>
      <TextField
        size="small"
        label={t("keys.profile.name")}
        value={name}
        onChange={(e) => setName(e.target.value)}
        helperText={t("keys.profile.nameHint")}
      />
      <TextField
        size="small"
        label={t("keys.profile.note")}
        value={note}
        onChange={(e) => setNote(e.target.value)}
        multiline
        minRows={2}
        helperText={t("keys.profile.noteHint")}
      />
      <Box>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={tag}
          onChange={(_e, v: KeyTag | "" | null) => setTag(v ?? "")}
          sx={{ "& .MuiToggleButton-root": { textTransform: "none" } }}
        >
          <ToggleButton value="">{t("keys.tags.none")}</ToggleButton>
          <ToggleButton value="staff">{t("keys.tags.staff")}</ToggleButton>
          <ToggleButton value="test">{t("keys.tags.test")}</ToggleButton>
        </ToggleButtonGroup>
        <Typography
          variant="caption"
          sx={{ display: "block", color: colors.text.secondary, mt: 0.5 }}
        >
          {t(`keys.tags.${tag || "none"}Help`)}
        </Typography>
      </Box>
      {crossesTest && (
        <Alert severity="warning">
          {t(tag === "test" ? "keys.tags.testOn" : "keys.tags.testOff")}
        </Alert>
      )}
      <Box>
        <Button
          variant="contained"
          size="small"
          disabled={!dirty || save.isPending}
          onClick={() => void submit()}
        >
          {t("keys.profile.save")}
        </Button>
      </Box>
    </Section>
  );
}

function Detail({ data }: Readonly<{ data: KeyDetailView }>) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const moderation = useModerationContext();
  const k = data.key;
  const days = lastDays(90);
  const values: Record<string, Record<string, number>> = {};
  data.days.forEach((d) => {
    values[d.day] = {
      share: d.share,
      vote: d.vote,
      report: d.report,
      mirror: d.mirror,
    };
  });
  const label = k.name || k.label;
  return (
    <>
      <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
        {k.banned ? (
          <Button
            size="small"
            variant="outlined"
            color="success"
            disabled={moderation.busy}
            onClick={() => moderation.unban(k.key_hmac, label)}
          >
            {t("keys.unban")}
          </Button>
        ) : (
          <Button
            size="small"
            variant="outlined"
            color="error"
            disabled={moderation.busy}
            onClick={() => moderation.ban(k.key_hmac, label)}
          >
            {t("keys.ban")}
          </Button>
        )}
        {!k.banned &&
          (k.trusted ? (
            <Button
              size="small"
              variant="outlined"
              disabled={moderation.busy}
              onClick={() => moderation.untrust(k.key_hmac, label)}
            >
              {t("keys.untrust")}
            </Button>
          ) : (
            <Button
              size="small"
              variant="outlined"
              color="info"
              disabled={moderation.busy}
              onClick={() => moderation.trust(k.key_hmac, label)}
            >
              {t("keys.trust")}
            </Button>
          ))}
      </Stack>
      {k.banned && k.ban_reason && (
        <Alert severity="error">
          {t("keys.banReason", { reason: k.ban_reason })}
        </Alert>
      )}
      <Facts
        items={[
          {
            label: t("keys.columns.firstSeen"),
            value: `${formatStamp(k.first_seen)} (${formatAgo(t, k.first_seen)})`,
          },
          {
            label: t("keys.columns.lastSeen"),
            value: k.last_seen
              ? `${formatStamp(k.last_seen)} (${formatAgo(t, k.last_seen)})`
              : t("keys.neverSent"),
          },
          {
            label: t("keys.detail.records"),
            value: data.kinds
              .map(
                (x) =>
                  `${t(`keys.kinds.${x.kind}`, { defaultValue: x.kind })}: ${String(x.count)}`,
              )
              .join(", "),
          },
          {
            label: t("keys.detail.reportsAgainst"),
            value: k.reports_against ? String(k.reports_against) : "",
          },
        ]}
      />
      <Section title={t("keys.detail.activity")}>
        <DailyBars
          days={days}
          values={values}
          series={[
            {
              key: "share",
              label: t("keys.kinds.share"),
              color: seriesColors.share,
            },
            {
              key: "vote",
              label: t("keys.kinds.vote"),
              color: seriesColors.vote,
            },
            {
              key: "report",
              label: t("keys.kinds.report"),
              color: seriesColors.report,
            },
            {
              key: "mirror",
              label: t("keys.kinds.mirror"),
              color: seriesColors.mirror,
            },
          ]}
        />
      </Section>
      <Profile data={data} />
      <Section title={t("keys.detail.sets", { count: data.sets.length })}>
        {data.sets.length === 0 ? (
          <EmptyState text={t("keys.detail.noSets")} />
        ) : (
          <Box sx={{ overflowX: "auto" }}>
            <Table size="small">
              <TableBody>
                {data.sets.map((s) => (
                  <TableRow key={s.set_id}>
                    <TableCell>
                      <SetRef
                        id={s.set_id}
                        title={s.title}
                        status={s.listed_version ? "active" : undefined}
                      />
                      <Box
                        sx={{
                          display: "flex",
                          gap: 0.5,
                          flexWrap: "wrap",
                          mt: 0.5,
                        }}
                      >
                        {s.versions.map((v) => (
                          <Chip
                            key={v.version}
                            size="small"
                            variant="outlined"
                            label={`v${String(v.version)} ${t(`status.set.${v.status}`)}`}
                            sx={{ height: 20, fontSize: 11 }}
                          />
                        ))}
                        {s.withheld && (
                          <Chip
                            size="small"
                            color="warning"
                            label={t(`sets.withheldReason.${s.withheld}`)}
                            sx={{ height: 20, fontSize: 11 }}
                          />
                        )}
                      </Box>
                    </TableCell>
                    <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                      {s.score
                        ? `${String(Math.round(s.score.score * 100))}% · n ${formatN(s.score.n)}`
                        : ""}
                    </TableCell>
                    <TableCell align="right">
                      {s.reports
                        ? t("keys.detail.reportsOn", { count: s.reports })
                        : ""}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        )}
      </Section>
      <Section title={t("keys.detail.votes", { count: data.votes_total })}>
        {data.votes.length === 0 ? (
          <EmptyState text={t("feedback.emptyVotes")} />
        ) : (
          <>
            <Box sx={{ overflowX: "auto" }}>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>{t("feedback.columns.when")}</TableCell>
                    <TableCell>{t("feedback.columns.set")}</TableCell>
                    <TableCell>{t("feedback.columns.kind")}</TableCell>
                    <TableCell align="right">
                      {t("feedback.columns.weight")}
                    </TableCell>
                    <TableCell>{t("feedback.columns.origin")}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {data.votes.map((v) => (
                    <TableRow key={v.id}>
                      <TableCell
                        sx={{ whiteSpace: "nowrap" }}
                        title={formatStamp(v.received_at)}
                      >
                        {formatAgo(t, v.received_at)}
                      </TableCell>
                      <TableCell>
                        <SetRef
                          id={v.set_id}
                          version={v.version}
                          title={v.title}
                          status={v.set_status}
                        />
                      </TableCell>
                      <TableCell>
                        <Chip
                          size="small"
                          variant="outlined"
                          color={v.weight >= 0 ? "success" : "error"}
                          label={t(`feedback.kinds.${v.kind}`, {
                            defaultValue: v.kind,
                          })}
                        />
                      </TableCell>
                      <TableCell align="right">
                        <AppliedWeight vote={v} />
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        {v.asn_observed
                          ? `AS${v.asn_observed} ${v.country_observed ?? ""}`
                          : ""}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>
            {data.votes_total > data.votes.length && (
              <Link
                component="button"
                type="button"
                underline="hover"
                variant="body2"
                onClick={() =>
                  void navigate(`/feedback?tab=votes&key=${k.key_hmac}`)
                }
              >
                {t("keys.detail.allVotes", { count: data.votes_total })}
              </Link>
            )}
          </>
        )}
      </Section>
      <Section title={t("keys.detail.reports", { count: data.reports.length })}>
        {data.reports.length === 0 ? (
          <EmptyState text={t("keys.detail.noReports")} />
        ) : (
          <Stack spacing={0.75}>
            {data.reports.map((r) => (
              <Box key={r.id}>
                <SetRef
                  id={r.set_id}
                  version={r.version}
                  title={r.title}
                  status={r.set_status}
                />
                <Typography
                  variant="caption"
                  sx={{ display: "block", color: colors.text.secondary }}
                >
                  {formatAgo(t, r.received_at)} ·{" "}
                  {t(`reports.state.${r.state}`)} ·{" "}
                  {r.reason || t("reports.noReason")}
                </Typography>
              </Box>
            ))}
          </Stack>
        )}
      </Section>
      <Section title={t("keys.detail.origins")}>
        {data.origins.length === 0 ? (
          <EmptyState text={t("keys.detail.noOrigins")} />
        ) : (
          <Box sx={{ overflowX: "auto" }}>
            <Table size="small">
              <TableBody>
                {data.origins.map((o) => (
                  <TableRow key={o.asn}>
                    <TableCell>
                      AS{o.asn} {o.country}
                      <Typography
                        variant="caption"
                        sx={{ display: "block", color: colors.text.secondary }}
                      >
                        {o.name}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      {o.sources
                        .map((s) => t(`keys.kinds.${s}`, { defaultValue: s }))
                        .join(", ")}
                    </TableCell>
                    <TableCell align="right">{o.count}</TableCell>
                    <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                      {formatAgo(t, o.last)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        )}
      </Section>
      {data.clients.length > 0 && (
        <Section title={t("keys.detail.clients")}>
          <Typography variant="body2">
            {data.clients
              .map(
                (c) =>
                  `b4 ${c.b4_version}${c.engine ? ` ${c.engine}` : ""} (${String(c.count)}, ${formatAgo(t, c.last)})`,
              )
              .join("; ")}
          </Typography>
        </Section>
      )}
      {data.mirrors.length > 0 && (
        <Section title={t("keys.detail.mirrors")}>
          {data.mirrors.map((m) => (
            <Typography key={m.id} variant="body2">
              {m.url} · {t(`status.mirror.${m.status}`)}
            </Typography>
          ))}
        </Section>
      )}
      <Section title={t("audit.history")}>
        <HistoryList entries={data.history} />
      </Section>
    </>
  );
}

export function KeyDetailDrawer({
  keyHmac,
  open,
  onClose,
}: Readonly<{ keyHmac: string | null; open: boolean; onClose: () => void }>) {
  const { t } = useTranslation();
  const { notify } = useSnackbar();
  const detail = useKeyDetail(keyHmac);
  const k = detail.data?.key;
  const copy = () => {
    if (!k) return;
    void navigator.clipboard.writeText(k.key_hmac).then(
      () => notify(t("app.copied"), "success"),
      () => undefined,
    );
  };
  return (
    <Drawer
      anchor="right"
      open={open && keyHmac !== null}
      onClose={onClose}
      slotProps={{
        paper: {
          sx: {
            width: { xs: "100%", md: 760 },
            maxWidth: "100%",
            bgcolor: colors.background.default,
          },
        },
      }}
    >
      <Box
        sx={{
          p: 3,
          display: "flex",
          flexDirection: "column",
          gap: 2.5,
          minWidth: 0,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2 }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="metricLabel">
              {t("keys.detail.heading")}
            </Typography>
            <Typography sx={{ fontSize: 20, fontWeight: 600, mt: 0.5 }}>
              {k?.name || k?.label || keyHmac?.slice(0, 16)}
            </Typography>
            {k && (
              <>
                <Box
                  sx={{
                    display: "flex",
                    alignItems: "center",
                    gap: 0.5,
                    mt: 0.5,
                  }}
                >
                  <Mono>{k.key_hmac}</Mono>
                  <Tooltip title={t("ref.copyKey")}>
                    <IconButton size="small" onClick={copy}>
                      <ContentCopyIcon sx={{ fontSize: 14 }} />
                    </IconButton>
                  </Tooltip>
                </Box>
                <Typography
                  variant="caption"
                  sx={{ color: colors.text.secondary, display: "block" }}
                >
                  {t("ref.authorLabel", { label: k.label })}
                </Typography>
                <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
                  <KeyStatus k={k} />
                  {k.tag && (
                    <Chip size="small" label={t(`keys.tags.${k.tag}`)} />
                  )}
                </Stack>
              </>
            )}
          </Box>
          <IconButton onClick={onClose} aria-label={t("app.close")}>
            <CloseIcon />
          </IconButton>
        </Box>
        {keyHmac !== null && (
          <QueryView query={detail}>
            {(data) => <Detail data={data} />}
          </QueryView>
        )}
      </Box>
    </Drawer>
  );
}

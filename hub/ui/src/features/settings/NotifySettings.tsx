import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Divider,
  FormControlLabel,
  FormGroup,
  Grid,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import NotificationsIcon from "@mui/icons-material/NotificationsOutlined";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { NotifyChannel, NotifyChannelStatusView, NotifyEvent, NotifyLanguage, NotifyRequest, NotifyView } from "@/models/api";
import { Section } from "@/shared/components/Section";
import { ErrorState, Loading } from "@/shared/components/States";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { useNotify, useSaveNotify, useTestNotify } from "./api";

interface Draft {
  telegram: boolean;
  chat: string;
  token: string;
  clearToken: boolean;
  webhook: boolean;
  url: string;
  clearUrl: boolean;
  secret: string;
  clearSecret: boolean;
  events: NotifyEvent[];
  digest: string;
  language: NotifyLanguage;
}

const toDraft = (v: NotifyView): Draft => ({
  telegram: v.telegram.enabled,
  chat: v.telegram.chat_id ?? "",
  token: "",
  clearToken: false,
  webhook: v.webhook.enabled,
  url: "",
  clearUrl: false,
  secret: "",
  clearSecret: false,
  events: v.events,
  digest: String(v.digest_seconds),
  language: v.language,
});

const toRequest = (d: Draft): NotifyRequest => ({
  telegram: { enabled: d.telegram, chat_id: d.chat.trim(), token: d.token.trim() || undefined, clear_token: d.clearToken || undefined },
  webhook: {
    enabled: d.webhook,
    url: d.url.trim() || undefined,
    clear_url: d.clearUrl || undefined,
    secret: d.secret.trim() || undefined,
    clear_secret: d.clearSecret || undefined,
  },
  events: d.events,
  digest_seconds: Number(d.digest),
  language: d.language,
});

const same = (a: Draft, b: Draft) => JSON.stringify(a) === JSON.stringify(b);

function SecretField({
  label,
  isSet,
  hint,
  fromEnv,
  envName,
  value,
  cleared,
  onChange,
  onClear,
  password = true,
}: Readonly<{
  label: string;
  isSet: boolean;
  hint?: string;
  fromEnv: boolean;
  envName: string;
  value: string;
  cleared: boolean;
  onChange: (v: string) => void;
  onClear: (v: boolean) => void;
  password?: boolean;
}>) {
  const { t } = useTranslation();
  const stored = isSet && !cleared;
  const helper = fromEnv
    ? t("notify.fromEnv", { name: envName })
    : cleared
      ? t("notify.willClear")
      : stored
        ? hint
          ? t("notify.storedHint", { hint })
          : t("notify.stored")
        : t("notify.notSet");
  return (
    <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
      <TextField
        size="small"
        fullWidth
        label={label}
        type={password ? "password" : "text"}
        autoComplete="off"
        value={value}
        disabled={fromEnv}
        placeholder={stored ? t("notify.keepPlaceholder") : ""}
        onChange={(e) => {
          onChange(e.target.value);
          if (cleared) onClear(false);
        }}
        helperText={helper}
      />
      {stored && !fromEnv && (
        <Button size="small" color="inherit" sx={{ mt: 0.5, flexShrink: 0 }} onClick={() => onClear(true)}>
          {t("notify.remove")}
        </Button>
      )}
    </Box>
  );
}

function ChannelStatus({ status }: Readonly<{ status?: NotifyChannelStatusView }>) {
  const { t } = useTranslation();
  if (!status || (!status.last_ok_at && !status.last_error_at)) {
    return (
      <Typography variant="caption" sx={{ color: colors.text.disabled }}>
        {t("notify.status.never")}
      </Typography>
    );
  }
  const failing = status.failures > 0;
  return (
    <Box>
      {status.last_ok_at && (
        <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }} title={formatStamp(status.last_ok_at)}>
          {t("notify.status.ok", { when: formatAgo(t, status.last_ok_at), count: status.sent_total })}
        </Typography>
      )}
      {failing && status.last_error && (
        <Typography variant="caption" sx={{ display: "block", color: colors.state.error, overflowWrap: "anywhere" }} title={formatStamp(status.last_error_at)}>
          {t("notify.status.failing", { count: status.failures, when: formatAgo(t, status.last_error_at), error: status.last_error })}
        </Typography>
      )}
    </Box>
  );
}

function Channel({
  channel,
  title,
  enabled,
  onEnabled,
  status,
  testing,
  canTest,
  onTest,
  children,
}: Readonly<{
  channel: NotifyChannel;
  title: string;
  enabled: boolean;
  onEnabled: (v: boolean) => void;
  status?: NotifyChannelStatusView;
  testing: boolean;
  canTest: boolean;
  onTest: (c: NotifyChannel) => void;
  children: React.ReactNode;
}>) {
  const { t } = useTranslation();
  return (
    <Box sx={{ border: `1px solid ${colors.border.light}`, borderRadius: 1, p: 2, display: "flex", flexDirection: "column", gap: 2, height: "100%" }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography sx={{ fontWeight: 600, flex: 1 }}>{title}</Typography>
        <FormControlLabel control={<Switch checked={enabled} onChange={(e) => onEnabled(e.target.checked)} />} label={t("notify.enabled")} />
      </Box>
      {children}
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", mt: "auto" }}>
        <Box sx={{ flex: 1, minWidth: 160 }}>
          <ChannelStatus status={status} />
        </Box>
        <Button size="small" variant="outlined" disabled={!canTest || testing} onClick={() => onTest(channel)}>
          {t("notify.test")}
        </Button>
      </Box>
    </Box>
  );
}

export function NotifySettings() {
  const { t } = useTranslation();
  const query = useNotify();
  const save = useSaveNotify();
  const test = useTestNotify();
  const { notify, notifyError } = useSnackbar();
  const [edited, setEdited] = useState<Draft | null>(null);

  const data = query.data;
  if (!data) {
    return query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : <Loading />;
  }
  const base = toDraft(data);
  const draft = edited ?? base;
  const dirty = !same(draft, base);
  const set = (patch: Partial<Draft>) => setEdited({ ...draft, ...patch });
  const digest = Number(draft.digest);
  const digestValid = Number.isInteger(digest) && digest >= data.digest_min && digest <= data.digest_max;

  const toggleEvent = (e: NotifyEvent) => set({ events: draft.events.includes(e) ? draft.events.filter((x) => x !== e) : data.available.filter((x) => x === e || draft.events.includes(x)) });

  const submit = async () => {
    try {
      await save.mutateAsync(toRequest(draft));
      setEdited(null);
      notify(t("notify.saved"), "success");
    } catch (err) {
      notifyError(err);
    }
  };

  const runTest = async (channel: NotifyChannel) => {
    try {
      await test.mutateAsync(channel);
      notify(t("notify.testSent"), "success");
    } catch (err) {
      notifyError(err);
    }
  };

  const tg = data.telegram;
  const wh = data.webhook;

  return (
    <Section title={t("notify.title")} description={t("notify.desc")} icon={<NotificationsIcon />}>
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Channel
            channel="telegram"
            title="Telegram"
            enabled={draft.telegram}
            onEnabled={(v) => set({ telegram: v })}
            status={data.status.telegram}
            testing={test.isPending}
            canTest={!dirty && tg.token_set && (tg.chat_id ?? "") !== ""}
            onTest={(c) => void runTest(c)}
          >
            <TextField
              size="small"
              label={t("notify.chat")}
              value={draft.chat}
              disabled={tg.chat_from_env}
              onChange={(e) => set({ chat: e.target.value })}
              helperText={tg.chat_from_env ? t("notify.fromEnv", { name: "B4HUB_TELEGRAM_CHAT" }) : t("notify.chatHint")}
            />
            <SecretField
              label={t("notify.token")}
              isSet={tg.token_set}
              hint={tg.token_hint}
              fromEnv={tg.token_from_env}
              envName="B4HUB_TELEGRAM_TOKEN"
              value={draft.token}
              cleared={draft.clearToken}
              onChange={(v) => set({ token: v })}
              onClear={(v) => set({ clearToken: v, token: "" })}
            />
          </Channel>
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Channel
            channel="webhook"
            title={t("notify.webhook")}
            enabled={draft.webhook}
            onEnabled={(v) => set({ webhook: v })}
            status={data.status.webhook}
            testing={test.isPending}
            canTest={!dirty && wh.url_set}
            onTest={(c) => void runTest(c)}
          >
            <SecretField
              label={t("notify.url")}
              isSet={wh.url_set}
              hint={wh.url_hint}
              fromEnv={wh.url_from_env}
              envName="B4HUB_WEBHOOK_URL"
              value={draft.url}
              cleared={draft.clearUrl}
              onChange={(v) => set({ url: v })}
              onClear={(v) => set({ clearUrl: v, url: "" })}
              password={false}
            />
            <SecretField
              label={t("notify.secret")}
              isSet={wh.secret_set}
              fromEnv={wh.secret_from_env}
              envName="B4HUB_WEBHOOK_SECRET"
              value={draft.secret}
              cleared={draft.clearSecret}
              onChange={(v) => set({ secret: v })}
              onClear={(v) => set({ clearSecret: v, secret: "" })}
            />
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("notify.signatureHint")}
            </Typography>
          </Channel>
        </Grid>
      </Grid>

      <Divider sx={{ my: 2, borderColor: colors.border.light }} />

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
            {t("notify.events")}
          </Typography>
          <FormGroup>
            {data.available.map((e) => (
              <FormControlLabel
                key={e}
                control={<Checkbox size="small" checked={draft.events.includes(e)} onChange={() => toggleEvent(e)} />}
                label={
                  <Box>
                    <Typography variant="body2">{t(`notify.event.${e}.label`)}</Typography>
                    <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                      {t(`notify.event.${e}.hint`)}
                    </Typography>
                  </Box>
                }
                sx={{ alignItems: "flex-start", mb: 0.5, "& .MuiCheckbox-root": { pt: 0.25 } }}
              />
            ))}
          </FormGroup>
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Stack spacing={2}>
            <TextField
              size="small"
              type="number"
              label={t("notify.digest")}
              value={draft.digest}
              onChange={(e) => set({ digest: e.target.value })}
              error={!digestValid}
              helperText={t("notify.digestHint", { min: data.digest_min, max: data.digest_max })}
              slotProps={{ htmlInput: { min: data.digest_min, max: data.digest_max } }}
            />
            <TextField select size="small" label={t("notify.language")} value={draft.language} onChange={(e) => set({ language: e.target.value as NotifyLanguage })}>
              <MenuItem value="en">English</MenuItem>
              <MenuItem value="ru">Русский</MenuItem>
            </TextField>
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("notify.testKeysHint")}
            </Typography>
          </Stack>
        </Grid>
      </Grid>

      {dirty && (
        <Alert severity="info" variant="outlined" sx={{ mt: 2 }}>
          {t("notify.unsaved")}
        </Alert>
      )}
      <Box sx={{ display: "flex", gap: 1, justifyContent: "flex-end", mt: 2 }}>
        <Button disabled={!dirty || save.isPending} color="inherit" onClick={() => setEdited(null)}>
          {t("notify.discard")}
        </Button>
        <Button variant="contained" disabled={!dirty || !digestValid || save.isPending} onClick={() => void submit()}>
          {t("notify.save")}
        </Button>
      </Box>
    </Section>
  );
}

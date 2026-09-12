import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  Box,
  CircularProgress,
  Grid,
  Link,
  Stack,
  Typography,
} from "@mui/material";
import IosShareIcon from "@mui/icons-material/IosShare";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import { MTProtoSecrets } from "./MTProtoSecrets";
import { MTProtoUpstreamCard } from "./MTProtoUpstream";
import { QRCodeSVG } from "qrcode.react";
import {
  DeleteIcon,
  DomainIcon,
  DownloadIcon,
  SniIcon,
  TelegramIcon,
  UploadIcon,
} from "@b4.icons";
import {
  B4Accordion,
  B4ConnectDetails,
  B4Dialog,
  B4Hint,
  B4IntegrationCard,
  B4NumberField,
  B4TextField,
} from "@b4.elements";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";
import { webProxyPageDownloadUrl } from "@api/mtproto";
import {
  useRemoveWebProxyPage,
  useUploadWebProxyPage,
  useWebProxyPage,
} from "@hooks/useWebProxyPage";

interface MTProtoSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

interface ShareTarget {
  name: string;
  secret: string;
}

const webSecretForm = (secret: string) =>
  "dd" + secret.trim().slice(2, 34).toLowerCase();

export const MTProtoSettings = ({ config, onChange }: MTProtoSettingsProps) => {
  const [share, setShare] = useState<ShareTarget | null>(null);

  const enabled = config.system.mtproto?.enabled ?? false;

  const openShare = (secretValue: string) => {
    const secrets = config.system.mtproto?.secrets ?? [];
    const entry = secrets.find((s) => s.secret === secretValue);
    setShare({ name: entry?.name || entry?.id || "", secret: secretValue });
  };

  return (
    <>
      <Stack spacing={2}>
        <ProxyCard config={config} onChange={onChange} />
        {enabled && (
          <>
            <AccessCard
              config={config}
              onChange={onChange}
              onShare={openShare}
            />
            <WebCarrierCard config={config} onChange={onChange} />
          </>
        )}
        <MTProtoUpstreamCard config={config} onChange={onChange} />
        <CreditLine />
      </Stack>
      {enabled && (
        <ShareDialog
          config={config}
          target={share}
          onClose={() => setShare(null)}
        />
      )}
    </>
  );
};

const ProxyCard = ({ config, onChange }: MTProtoSettingsProps) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const enabled = mtproto?.enabled ?? false;

  return (
    <B4IntegrationCard
      icon={<TelegramIcon />}
      title={t("settings.MTProto.title")}
      description={t("settings.MTProto.serverDesc")}
      enabled={enabled}
      onToggle={(checked) => onChange("system.mtproto.enabled", checked)}
      toggleLabel={t("settings.MTProto.enable")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("settings.MTProto.bindAddress")}
            value={mtproto?.bind_address || "0.0.0.0"}
            onChange={(e) =>
              onChange("system.mtproto.bind_address", e.target.value)
            }
            placeholder={t("settings.MTProto.bindAddressPlaceholder")}
            helperText={t("settings.MTProto.bindAddressHelp")}
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4NumberField
            label={t("settings.MTProto.port")}
            value={mtproto?.port ?? 3128}
            onChange={(n) => onChange("system.mtproto.port", n)}
            min={1}
            max={65535}
          />
        </Grid>
        <Grid size={{ xs: 12 }}>
          <B4TextField
            label={t("settings.MTProto.fakeSNI")}
            value={mtproto?.fake_sni || "storage.googleapis.com"}
            onChange={(e) => onChange("system.mtproto.fake_sni", e.target.value)}
            helperText={t("settings.MTProto.fakeSNIHelp")}
          />
        </Grid>
      </Grid>

      <B4Accordion title={t("settings.MTProto.advanced")}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.maxConnections")}
              value={mtproto?.max_connections || 2048}
              onChange={(n) => onChange("system.mtproto.max_connections", n)}
              min={16}
              max={100000}
              helperText={t("settings.MTProto.maxConnectionsHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.tcpUserTimeout")}
              value={mtproto?.tcp_user_timeout_sec || 120}
              onChange={(n) =>
                onChange("system.mtproto.tcp_user_timeout_sec", n)
              }
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.tcpUserTimeoutHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.MTProto.idleTimeout")}
              value={mtproto?.idle_timeout_sec || 300}
              onChange={(n) => onChange("system.mtproto.idle_timeout_sec", n)}
              min={-1}
              max={86400}
              helperText={t("settings.MTProto.idleTimeoutHelp")}
            />
          </Grid>
        </Grid>
      </B4Accordion>
    </B4IntegrationCard>
  );
};

const AccessCard = ({
  config,
  onChange,
  onShare,
}: MTProtoSettingsProps & { onShare: (secret: string) => void }) => {
  const { t } = useTranslation();

  return (
    <B4IntegrationCard
      icon={<SniIcon />}
      title={t("settings.MTProto.secretsTitle")}
      description={t("settings.MTProto.secretsDesc")}
    >
      <MTProtoSecrets config={config} onChange={onChange} onShare={onShare} />
    </B4IntegrationCard>
  );
};

const WebCarrierCard = ({ config, onChange }: MTProtoSettingsProps) => {
  const { t } = useTranslation();
  const webProxy = config.system.mtproto?.web_proxy;
  const hostname = (webProxy?.hostname || "").trim();
  const enabled = webProxy?.enabled ?? false;
  const port = webProxy?.port ?? 0;
  const webServerPort = config.system.web_server?.port ?? 0;
  const portClash = port > 0 && port === webServerPort;

  return (
    <B4IntegrationCard
      icon={<DomainIcon />}
      title={t("settings.MTProto.webProxyTitle")}
      description={t("settings.MTProto.webProxyDesc")}
      enabled={enabled}
      onToggle={(checked) =>
        onChange("system.mtproto.web_proxy.enabled", checked)
      }
      toggleLabel={t("settings.MTProto.webProxyEnable")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4TextField
            label={t("settings.MTProto.webProxyHostname")}
            value={webProxy?.hostname || ""}
            onChange={(e) =>
              onChange("system.mtproto.web_proxy.hostname", e.target.value)
            }
            placeholder="relay.example.org"
            error={!hostname}
            helperText={
              hostname
                ? t("settings.MTProto.webProxyHostnameHelp")
                : t("settings.MTProto.webProxyNoHost")
            }
            selectOnFocus
          />
        </Grid>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4NumberField
            label={t("settings.MTProto.webProxyPort")}
            value={port}
            onChange={(n) => onChange("system.mtproto.web_proxy.port", n)}
            min={0}
            max={65535}
            error={portClash}
            helperText={
              portClash
                ? t("settings.MTProto.webProxyPortClash")
                : port > 0
                  ? t("settings.MTProto.webProxyPortOwn")
                  : t("settings.MTProto.webProxyPortShared")
            }
          />
        </Grid>
      </Grid>
      <Grid container>
        <B4Hint>
          {port > 0
            ? t("settings.MTProto.webProxyRequirementsOwnPort")
            : t("settings.MTProto.webProxyRequirements")}
        </B4Hint>
      </Grid>
      <B4Accordion title={t("settings.MTProto.webProxyAdvanced")}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4TextField
              label={t("settings.MTProto.webProxyTlsCert")}
              value={webProxy?.tls_cert || ""}
              onChange={(e) =>
                onChange("system.mtproto.web_proxy.tls_cert", e.target.value)
              }
              placeholder="/etc/b4/relay.crt"
              helperText={t("settings.MTProto.webProxyTlsCertHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4TextField
              label={t("settings.MTProto.webProxyTlsKey")}
              value={webProxy?.tls_key || ""}
              onChange={(e) =>
                onChange("system.mtproto.web_proxy.tls_key", e.target.value)
              }
              placeholder="/etc/b4/relay.key"
              helperText={t("settings.MTProto.webProxyTlsKeyHelp")}
            />
          </Grid>
        </Grid>
      </B4Accordion>
      <WebProxyPagePanel enabled={enabled} hostname={hostname} />
    </B4IntegrationCard>
  );
};

const WebProxyPagePanel = ({
  enabled,
  hostname,
}: {
  enabled: boolean;
  hostname: string;
}) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const page = useWebProxyPage(enabled);
  const upload = useUploadWebProxyPage();
  const remove = useRemoveWebProxyPage();
  const custom = page.data?.custom ?? false;
  const busy = upload.isPending || remove.isPending;

  const onUpload = (file: File) => {
    upload.mutate(file, {
      onSuccess: () => showSuccess(t("settings.MTProto.webProxyPageUploaded")),
      onError: (e) => showError(describeApiError(e)),
    });
  };
  const onRemove = () => {
    remove.mutate(undefined, {
      onSuccess: () => showSuccess(t("settings.MTProto.webProxyPageRemoved")),
      onError: (e) => showError(describeApiError(e)),
    });
  };

  return (
    <Box sx={{ mt: 2 }}>
      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        {t("settings.MTProto.webProxyPageTitle")}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        {t("settings.MTProto.webProxyPageDesc")}
      </Typography>
      <Typography variant="body2" sx={{ mb: 1 }}>
        {custom
          ? t("settings.MTProto.webProxyPageCustom", {
              size: page.data?.size ?? 0,
            })
          : t("settings.MTProto.webProxyPageBuiltin")}
      </Typography>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        <Button
          variant="outlined"
          size="small"
          startIcon={
            upload.isPending ? <CircularProgress size={16} /> : <UploadIcon />
          }
          onClick={() => fileInputRef.current?.click()}
          disabled={busy}
        >
          {upload.isPending ? t("core.uploading") : t("core.upload")}
        </Button>
        <Button
          variant="outlined"
          size="small"
          startIcon={<DownloadIcon />}
          component="a"
          href={webProxyPageDownloadUrl}
          disabled={!custom || busy}
        >
          {t("core.download")}
        </Button>
        <Button
          variant="outlined"
          size="small"
          color="error"
          startIcon={
            remove.isPending ? <CircularProgress size={16} /> : <DeleteIcon />
          }
          onClick={onRemove}
          disabled={!custom || busy}
        >
          {t("settings.MTProto.webProxyPageRemove")}
        </Button>
        {hostname && (
          <Button
            variant="text"
            size="small"
            endIcon={<OpenInNewIcon />}
            component="a"
            href={`https://${hostname}/`}
            target="_blank"
            rel="noreferrer"
          >
            {t("settings.MTProto.webProxyPageOpen")}
          </Button>
        )}
        <input
          ref={fileInputRef}
          type="file"
          accept=".html,.htm,text/html"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) onUpload(file);
            e.target.value = "";
          }}
        />
      </Box>
      <Grid container sx={{ mt: 1 }}>
        <B4Hint>{t("settings.MTProto.webProxyPageHint")}</B4Hint>
      </Grid>
    </Box>
  );
};

const ShareDialog = ({
  config,
  target,
  onClose,
}: {
  config: B4Config;
  target: ShareTarget | null;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const port = mtproto?.port ?? 3128;
  const webProxy = mtproto?.web_proxy;
  const webHost = (webProxy?.hostname || "").trim().toLowerCase();
  const webEnabled = (webProxy?.enabled ?? false) && webHost.length > 0;
  const bindAddress = mtproto?.bind_address || "";

  const [host, setHost] = useState("");

  useEffect(() => {
    if (!target) return;
    const isAnyAddr =
      !bindAddress || bindAddress === "0.0.0.0" || bindAddress === "::";
    setHost(isAnyAddr ? globalThis.location.hostname : bindAddress);
  }, [target, bindAddress]);

  const secret = target?.secret ?? "";

  const directLink = useMemo(() => {
    const h = host.trim();
    if (!h || !secret) return "";
    return `tg://proxy?server=${encodeURIComponent(h)}&port=${port}&secret=${encodeURIComponent(secret)}`;
  }, [host, port, secret]);

  const webLink = useMemo(() => {
    if (!webEnabled || secret.trim().length < 34) return "";
    return `https://t.me/webproxy?server=${webHost}&secret=${webSecretForm(secret)}`;
  }, [webEnabled, webHost, secret]);

  const canShare =
    typeof navigator !== "undefined" && typeof navigator.share === "function";

  const handleNativeShare = async () => {
    if (!directLink || !canShare) return;
    try {
      await navigator.share({
        title: t("settings.MTProto.title"),
        url: directLink,
      });
    } catch {
      /* user cancelled */
    }
  };

  return (
    <B4Dialog
      open={target !== null}
      onClose={onClose}
      fullWidth
      maxWidth="sm"
      title={
        target?.name
          ? t("settings.MTProto.shareDialogTitleNamed", { name: target.name })
          : t("settings.MTProto.shareDialogTitle")
      }
      icon={<IosShareIcon />}
      actions={
        <>
          <Button onClick={onClose}>{t("core.close")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            component="a"
            variant="outlined"
            href={directLink || "#"}
            target="_blank"
            rel="noreferrer"
            startIcon={<OpenInNewIcon />}
            disabled={!directLink}
          >
            {t("settings.MTProto.shareOpen")}
          </Button>
          {canShare && (
            <Button
              variant="contained"
              startIcon={<IosShareIcon />}
              onClick={() => void handleNativeShare()}
              disabled={!directLink}
            >
              {t("settings.MTProto.shareNative")}
            </Button>
          )}
        </>
      }
    >
      <B4TextField
        sx={{ mt: 3 }}
        label={t("settings.MTProto.shareHost")}
        value={host}
        onChange={(e) => setHost(e.target.value)}
        helperText={t("settings.MTProto.shareHostHelp")}
        autoFocus
      />

      {directLink && (
        <B4ConnectDetails
          label={t("settings.MTProto.shareDirectLabel")}
          snippet={directLink}
        />
      )}

      {webLink && (
        <B4ConnectDetails
          label={t("settings.MTProto.shareWebLabel")}
          snippet={webLink}
          footer={t("settings.MTProto.shareWebFooter")}
        />
      )}

      {directLink && (
        <Box
          sx={{
            px: 1,
            pt: 1,
            bgcolor: "#fff",
            borderRadius: 2,
            alignSelf: "center",
          }}
        >
          <QRCodeSVG
            value={directLink}
            size={220}
            level="H"
            marginSize={0}
            imageSettings={{
              src: "/favicon.svg",
              height: 32,
              width: 32,
              excavate: true,
            }}
          />
        </Box>
      )}
    </B4Dialog>
  );
};

const CreditLine = () => {
  const { t } = useTranslation();
  return (
    <Typography variant="caption" sx={{ px: 0.5 }}>
      {t("settings.MTProto.credit")}{" "}
      <Link
        href="https://github.com/Flowseal/tg-ws-proxy"
        target="_blank"
        rel="noreferrer"
        sx={{ display: "inline-flex", alignItems: "center", gap: 0.25 }}
      >
        tg-ws-proxy
        <OpenInNewIcon sx={{ fontSize: 12 }} />
      </Link>
    </Typography>
  );
};

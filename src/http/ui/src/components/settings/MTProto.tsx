import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button, Box, Grid, Link, Stack, Typography } from "@mui/material";
import IosShareIcon from "@mui/icons-material/IosShare";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import { MTProtoSecrets } from "./MTProtoSecrets";
import { MTProtoUpstreamCard } from "./MTProtoUpstream";
import { QRCodeSVG } from "qrcode.react";
import { DomainIcon, SniIcon, TelegramIcon } from "@b4.icons";
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
      <Grid container>
        <B4Hint>{t("settings.MTProto.webProxyRequirements")}</B4Hint>
      </Grid>
    </B4IntegrationCard>
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

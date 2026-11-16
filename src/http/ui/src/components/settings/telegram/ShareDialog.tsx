import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  Box,
  Chip,
  CircularProgress,
  Grid,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@mui/material";
import IosShareIcon from "@mui/icons-material/IosShare";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import { QRCodeSVG } from "qrcode.react";
import {
  B4Alert,
  B4ConnectDetails,
  B4Dialog,
  B4Hint,
  B4TextField,
} from "@b4.elements";
import { B4Config, MTProtoConfig } from "@models/config";
import { describeApiError } from "@utils";
import { useSystemAddresses } from "@hooks/useSystemAddresses";
import {
  type HostCandidate,
  type ShareNote,
  type ShareScope,
  type ShareTarget,
  internetCandidates,
  isAnyAddress,
  isLoopbackAddress,
  parseEndpoint,
  parsePort,
  shareNotes,
  webSecretForm,
} from "./share";

interface TypedState {
  target: ShareTarget | null;
  scope?: ShareScope;
  hosts: Partial<Record<ShareScope, string>>;
  ports: Partial<Record<ShareScope, string>>;
}

interface ShareDialogProps {
  config: B4Config;
  savedMtproto?: MTProtoConfig;
  target: ShareTarget | null;
  onClose: () => void;
}

export const ShareDialog = ({
  config,
  savedMtproto,
  target,
  onClose,
}: ShareDialogProps) => {
  const { t } = useTranslation();
  const mtproto = config.system.mtproto;
  const running = savedMtproto ?? mtproto;
  const port = running?.port ?? 3128;
  const webProxy = mtproto?.web_proxy;
  const webHost = (webProxy?.hostname || "").trim().toLowerCase();
  const webEnabled = (webProxy?.enabled ?? false) && webHost.length > 0;
  const bindAddress = running?.bind_address || "";
  const bindSpecific = !isAnyAddress(bindAddress);
  const bindLoopback = isLoopbackAddress(bindAddress);
  const publicHost = (mtproto?.public_host ?? "").trim();
  const publicPort = mtproto?.public_port ?? 0;
  const hasPublicHost = publicHost.length > 0;

  const [typed, setTyped] = useState<TypedState>({
    target: null,
    hosts: {},
    ports: {},
  });
  const current: TypedState =
    target === null || typed.target === target
      ? typed
      : { target, hosts: {}, ports: {} };

  const defaultScope: ShareScope =
    hasPublicHost || bindLoopback ? "internet" : "local";
  const scope = current.scope ?? defaultScope;
  const internet = scope === "internet";
  const usingPublic = internet && hasPublicHost;
  const listenerPortUsed = !(internet && publicPort > 0);
  const unsaved =
    savedMtproto !== undefined &&
    ((listenerPortUsed &&
      (mtproto?.port ?? 3128) !== (savedMtproto.port ?? 3128)) ||
      (!usingPublic &&
        ((mtproto?.bind_address || "") !==
          (savedMtproto.bind_address || "") ||
          (mtproto?.expose ?? false) !== (savedMtproto.expose ?? false))));

  const lookup = target !== null && internet && !hasPublicHost;
  const addresses = useSystemAddresses(lookup, false);
  const addrs = addresses.data;
  const wan4 = addrs?.wan_v4;
  const wan6 = addrs?.wan_v6;
  const bindBehindNat =
    bindSpecific && bindAddress === wan4?.ip && wan4?.scope !== "public";
  const needsProbe =
    lookup &&
    addresses.isSuccess &&
    (!bindSpecific || bindBehindNat) &&
    wan4?.scope !== "public";
  const probe = useSystemAddresses(needsProbe, true);
  const publicV4 = needsProbe ? (probe.data?.public_v4 ?? "") : "";
  const probeSettled = !needsProbe || probe.isSuccess || probe.isError;

  let publicError: string | null = null;
  if (needsProbe && probe.isError) {
    publicError = describeApiError(probe.error);
  } else if (needsProbe && probe.data && !publicV4) {
    publicError = probe.data.public_error ?? "";
  }

  const publicWan4 = wan4?.scope === "public" ? wan4.ip : "";
  const publicWan6 = wan6?.scope === "public" ? wan6.ip : "";
  const behindCgnat = wan4?.scope === "cgnat";
  let autoInternet: string;
  if (hasPublicHost) {
    autoInternet = publicHost;
  } else if (bindLoopback) {
    autoInternet = "";
  } else if (bindBehindNat) {
    autoInternet = probeSettled ? publicV4 || bindAddress : "";
  } else if (bindSpecific) {
    autoInternet = bindAddress;
  } else {
    autoInternet =
      publicWan4 ||
      (behindCgnat ? publicWan6 : "") ||
      (probeSettled ? publicV4 || publicWan6 : "");
  }
  const autoLocal = bindSpecific ? bindAddress : globalThis.location.hostname;
  const autoHost = internet ? autoInternet : autoLocal;

  const autoPort = internet && publicPort > 0 ? publicPort : port;
  const host = current.hosts[scope] ?? autoHost;
  const portText = current.ports[scope] ?? String(autoPort);
  const setHost = (value: string | undefined) =>
    setTyped({
      ...current,
      target,
      hosts: { ...current.hosts, [scope]: value },
    });
  const setPortText = (value: string) =>
    setTyped({
      ...current,
      target,
      ports: { ...current.ports, [scope]: value },
    });

  const endpoint = parseEndpoint(host);
  const linkHost = endpoint.host;
  const hostHasPort =
    endpoint.port !== undefined || endpoint.invalidPort === true;
  const linkPort = endpoint.invalidPort
    ? undefined
    : (endpoint.port ?? parsePort(portText));
  const portFieldInvalid = !hostHasPort && linkPort === undefined;
  const hostIsV6 = linkHost.includes(":");
  const splitTypedPort = () => {
    if (endpoint.port === undefined) return;
    setTyped({
      ...current,
      target,
      hosts: { ...current.hosts, [scope]: endpoint.host },
      ports: { ...current.ports, [scope]: String(endpoint.port) },
    });
  };
  let candidates: HostCandidate[] = [];
  if (internet && bindBehindNat) {
    candidates = internetCandidates(
      addrs ? { ...addrs, wan_v6: undefined } : addrs,
      publicV4,
    );
  } else if (internet && !bindSpecific) {
    candidates = internetCandidates(addrs, publicV4);
  }
  const savedOn = savedMtproto ? savedMtproto.enabled : true;
  const secretSaved = savedMtproto
    ? (savedMtproto.secrets ?? []).some(
        (s) => s.enabled && s.secret === (target?.secret ?? ""),
      )
    : true;
  const proxyRunning = savedOn && secretSaved;
  const unsavedNote: ShareNote[] =
    unsaved && proxyRunning
      ? [
          {
            id: "unsaved",
            severity: "info",
            text: t(
              linkPort === port
                ? "settings.MTProto.shareUnsaved"
                : "settings.MTProto.shareUnsavedRunning",
              { port },
            ),
          },
        ]
      : [];
  const stoppedNote: ShareNote[] =
    !proxyRunning && !(internet && !hasPublicHost)
      ? [
          {
            id: "stopped",
            severity: "info",
            text: t("settings.MTProto.shareNotSaved"),
          },
        ]
      : [];
  const publicNote: ShareNote[] =
    usingPublic && linkHost === publicHost
      ? [
          {
            id: "public",
            severity: "info",
            text: t("settings.MTProto.sharePublicAddress", { port }),
          },
        ]
      : [];
  const loopbackNote: ShareNote[] =
    !internet && bindLoopback
      ? [
          {
            id: "loopback",
            severity: "info",
            text: t(
              hasPublicHost
                ? "settings.MTProto.shareLoopbackPublic"
                : "settings.MTProto.shareLoopbackLocal",
              { bind: bindAddress },
            ),
          },
        ]
      : [];
  const notes = unsavedNote.concat(
    stoppedNote,
    publicNote,
    loopbackNote,
    internet && !hasPublicHost
      ? shareNotes(t, {
          addrs,
          addrError: addresses.isError
            ? describeApiError(addresses.error)
            : null,
          publicError,
          port,
          bindAddress,
          hostIsV6,
          proxyRunning,
        })
      : [],
  );
  const detecting =
    internet && !hasPublicHost && (addresses.isLoading || probe.isLoading);

  const secret = target?.secret ?? "";

  const directLink = useMemo(() => {
    if (!linkHost || !linkPort || !secret) return "";
    return `tg://proxy?server=${encodeURIComponent(linkHost)}&port=${linkPort}&secret=${encodeURIComponent(secret)}`;
  }, [linkHost, linkPort, secret]);

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
      <Stack spacing={2} sx={{ mt: 1 }}>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={scope}
          onChange={(_, v: ShareScope | null) =>
            v && setTyped({ ...current, target, scope: v })
          }
          sx={{ alignSelf: "flex-start" }}
        >
          <ToggleButton value="local">
            {t("settings.MTProto.shareScopeLocal")}
          </ToggleButton>
          <ToggleButton value="internet">
            {t("settings.MTProto.shareScopeInternet")}
          </ToggleButton>
        </ToggleButtonGroup>

        <Grid container spacing={2}>
          <Grid size={{ xs: 12, sm: 8 }}>
            <B4TextField
              label={t("settings.MTProto.shareHost")}
              value={host}
              onChange={(e) => setHost(e.target.value)}
              onBlur={splitTypedPort}
              error={endpoint.invalidPort === true}
              helperText={t(
                endpoint.invalidPort
                  ? "settings.MTProto.shareHostPortInvalid"
                  : usingPublic && linkHost === publicHost
                    ? "settings.MTProto.shareHostHelpPublic"
                    : internet
                      ? "settings.MTProto.shareHostHelpInternet"
                      : "settings.MTProto.shareHostHelp",
              )}
              autoFocus
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 4 }}>
            <B4TextField
              label={t("settings.MTProto.sharePort")}
              value={
                endpoint.port !== undefined
                  ? String(endpoint.port)
                  : hostHasPort
                    ? ""
                    : portText
              }
              onChange={(e) =>
                setPortText(e.target.value.replaceAll(/\D/g, "").slice(0, 5))
              }
              disabled={hostHasPort}
              inputMode="numeric"
              error={portFieldInvalid}
              helperText={
                portFieldInvalid
                  ? t("settings.MTProto.sharePortInvalid")
                  : undefined
              }
            />
          </Grid>
        </Grid>

        {candidates.length > 0 && (
          <Box>
            <Typography variant="caption" color="text.secondary">
              {t("settings.MTProto.shareCandidates")}
            </Typography>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mt: 0.5 }}>
              {candidates.map((c) => {
                const selected = c.ip === linkHost;
                const source = c.iface
                  ? t("settings.MTProto.shareSourceWan", { iface: c.iface })
                  : t("settings.MTProto.shareSourcePublic");
                return (
                  <Chip
                    key={c.ip}
                    size="small"
                    clickable
                    color={selected ? "primary" : "default"}
                    variant={selected ? "filled" : "outlined"}
                    label={`${c.ip} · ${source}`}
                    onClick={() =>
                      setHost(c.ip === autoHost ? undefined : c.ip)
                    }
                    sx={{ maxWidth: "100%" }}
                  />
                );
              })}
            </Box>
          </Box>
        )}

        {detecting && (
          <Stack direction="row" alignItems="center" gap={1}>
            <CircularProgress size={16} />
            <Typography variant="body2" color="text.secondary">
              {t(
                addresses.isLoading
                  ? "settings.MTProto.shareDetecting"
                  : "settings.MTProto.shareDetectingPublic",
              )}
            </Typography>
          </Stack>
        )}

        {notes.map((n) => (
          <B4Alert key={n.id} severity={n.severity}>
            {n.text}
          </B4Alert>
        ))}

        {internet && (
          <Grid container>
            <B4Hint>{t("settings.MTProto.shareTestHint")}</B4Hint>
          </Grid>
        )}

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
      </Stack>
    </B4Dialog>
  );
};

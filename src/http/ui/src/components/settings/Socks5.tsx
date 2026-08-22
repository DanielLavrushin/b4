import { useMemo, useState } from "react";
import { Box, Button, Menu, MenuItem, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { ConnectionIcon, DeviceIcon } from "@b4.icons";
import {
  B4Alert,
  B4ChipList,
  B4FormGroup,
  B4NumberField,
  B4PlusButton,
  B4Section,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { useDevices } from "@b4.devices";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface Socks5SettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

type SourceIssue = "invalid" | "all" | null;

const IPV4_RE =
  /^(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(\.(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])){3}$/;

const isIPv4 = (host: string) => IPV4_RE.test(host);

const isIPv6 = (host: string) =>
  host.includes(":") && /^[0-9a-fA-F:.]+$/.test(host);

const sourceIssue = (raw: string): SourceIssue => {
  const value = raw.trim();
  if (!value) return "invalid";
  const parts = value.split("/");
  if (parts.length > 2) return "invalid";
  const v4 = isIPv4(parts[0]);
  const v6 = !v4 && isIPv6(parts[0]);
  if (!v4 && !v6) return "invalid";
  if (parts.length === 1) return null;
  if (!/^[0-9]{1,3}$/.test(parts[1])) return "invalid";
  const bits = Number(parts[1]);
  if (bits > (v4 ? 32 : 128)) return "invalid";
  return bits === 0 ? "all" : null;
};

export const Socks5Settings = ({ config, onChange }: Socks5SettingsProps) => {
  const { t } = useTranslation();
  const [draft, setDraft] = useState("");
  const [deviceAnchor, setDeviceAnchor] = useState<HTMLElement | null>(null);
  const { devices, loading: devicesLoading, loadDevices } = useDevices();

  const socks5 = config.system.socks5;
  const enabled = socks5?.enabled ?? false;
  const username = socks5?.username || "";
  const password = socks5?.password || "";
  const sources = useMemo(
    () => socks5?.allowed_sources ?? [],
    [socks5?.allowed_sources],
  );

  const draftIssue: SourceIssue = draft.trim() ? sourceIssue(draft) : null;

  const deviceEntries = useMemo(
    () =>
      devices
        .filter((d) => isIPv4(d.ip))
        .map((d) => ({
          ip: d.ip,
          label: d.alias || d.hostname || d.ip,
          entry: `${d.ip}/32`,
        }))
        .filter((d) => !sources.includes(d.entry)),
    [devices, sources],
  );

  const setSources = (next: string[]) =>
    onChange("system.socks5.allowed_sources", next);

  const addSource = (raw: string) => {
    const value = raw.trim();
    if (!value || sourceIssue(value)) return;
    if (!sources.includes(value)) setSources([...sources, value]);
    setDraft("");
  };

  const removeSource = (value: string) =>
    setSources(sources.filter((s) => s !== value));

  const openDevices = (e: React.MouseEvent<HTMLElement>) => {
    setDeviceAnchor(e.currentTarget);
    loadDevices().catch(() => {});
  };

  const pickDevice = (entry: string) => {
    setDeviceAnchor(null);
    if (!sources.includes(entry)) setSources([...sources, entry]);
  };

  const draftHelper = () => {
    if (draftIssue === "invalid") return t("settings.Socks5.sourceInvalid");
    if (draftIssue === "all") return t("settings.Socks5.sourceAll");
    return t("settings.Socks5.addSourceHelp");
  };

  const deviceMenuItems = () => {
    if (devicesLoading) {
      return <MenuItem disabled>{t("core.loading")}</MenuItem>;
    }
    if (deviceEntries.length === 0) {
      return <MenuItem disabled>{t("settings.Socks5.noDevices")}</MenuItem>;
    }
    return deviceEntries.map((d) => (
      <MenuItem key={d.ip} onClick={() => pickDevice(d.entry)}>
        {d.label} - {d.entry}
      </MenuItem>
    ));
  };

  const openRelay = enabled && sources.length === 0 && !username && !password;

  return (
    <B4Section
      title={t("settings.Socks5.title")}
      description={t("settings.Socks5.description")}
      icon={<ConnectionIcon />}
    >
      <B4FormGroup label={t("settings.Socks5.settings")} columns={2}>
        <B4Switch
          label={t("settings.Socks5.enable")}
          checked={enabled}
          onChange={(checked: boolean) =>
            onChange("system.socks5.enabled", checked)
          }
          description={t("settings.Socks5.enableDesc")}
        />
        <B4TextField
          label={t("settings.Socks5.bindAddress")}
          value={socks5?.bind_address || "0.0.0.0"}
          onChange={(e) =>
            onChange("system.socks5.bind_address", e.target.value)
          }
          placeholder={t("settings.Socks5.bindAddressPlaceholder")}
          disabled={!enabled}
          helperText={t("settings.Socks5.bindAddressHelp")}
          selectOnFocus
        />
        <B4NumberField
          label={t("settings.Socks5.port")}
          value={socks5?.port ?? 1080}
          onChange={(n) => onChange("system.socks5.port", n)}
          min={1}
          max={65535}
          disabled={!enabled}
          helperText={t("settings.Socks5.portHelp")}
        />
        <B4TextField
          label={t("settings.Socks5.username")}
          value={username}
          onChange={(e) => onChange("system.socks5.username", e.target.value)}
          disabled={!enabled}
          helperText={t("settings.Socks5.usernameHelp")}
          autoComplete="new-password"
        />
        <B4TextField
          label={t("settings.Socks5.password")}
          type="password"
          value={password}
          onChange={(e) => onChange("system.socks5.password", e.target.value)}
          disabled={!enabled}
          helperText={t("settings.Socks5.passwordHelp")}
          autoComplete="new-password"
        />
      </B4FormGroup>

      <B4FormGroup
        label={t("settings.Socks5.sources")}
        description={t("settings.Socks5.sourcesDesc")}
        columns={1}
      >
        <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
          <B4TextField
            label={t("settings.Socks5.addSource")}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addSource(draft);
              }
            }}
            placeholder={t("settings.Socks5.addSourcePlaceholder")}
            disabled={!enabled}
            error={!!draftIssue}
            helperText={draftHelper()}
          />
          <B4PlusButton
            onClick={() => addSource(draft)}
            disabled={!enabled || !draft.trim() || !!draftIssue}
          />
          <Button
            size="small"
            startIcon={<DeviceIcon />}
            onClick={openDevices}
            disabled={!enabled}
            sx={{ mt: 0.75, whiteSpace: "nowrap" }}
          >
            {t("settings.Socks5.fromDevice")}
          </Button>
        </Box>

        {sources.length > 0 ? (
          <B4ChipList
            items={sources}
            getKey={(s) => s}
            getLabel={(s) => s}
            onDelete={enabled ? removeSource : undefined}
            title={t("settings.Socks5.activeSources")}
            collapsedMax={20}
          />
        ) : (
          <Typography variant="body2" color="text.secondary">
            {t("settings.Socks5.sourcesEmpty")}
          </Typography>
        )}

        <B4Alert severity="info">{t("settings.Socks5.sourcesNote")}</B4Alert>

        {openRelay && (
          <B4Alert severity="warning">
            {t("settings.Socks5.openRelayWarning")}
          </B4Alert>
        )}
      </B4FormGroup>

      <Menu
        anchorEl={deviceAnchor}
        open={!!deviceAnchor}
        onClose={() => setDeviceAnchor(null)}
      >
        {deviceMenuItems()}
      </Menu>
    </B4Section>
  );
};

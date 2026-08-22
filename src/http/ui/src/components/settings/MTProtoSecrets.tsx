import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Box,
  Button,
  CircularProgress,
  IconButton,
  Paper,
  Switch,
  Tooltip,
} from "@mui/material";
import IosShareIcon from "@mui/icons-material/IosShare";
import { AddIcon, DeleteIcon } from "@b4.icons";
import { B4SecretField, B4TextField } from "@b4.elements";
import { B4Config, MTProtoSecret } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";

interface MTProtoSecretsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
  onShare: (secret: string) => void;
}

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `s-${Date.now()}-${Math.floor(Math.random() * 1e6)}`;

const currentSecrets = (config: B4Config): MTProtoSecret[] =>
  config.system.mtproto?.secrets ?? [];

export const MTProtoSecrets = ({
  config,
  onChange,
  onShare,
}: MTProtoSecretsProps) => {
  const { t } = useTranslation();
  const enabled = config.system.mtproto?.enabled ?? false;
  const secrets = currentSecrets(config);
  const [generatingIdx, setGeneratingIdx] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);

  const commit = (next: MTProtoSecret[]) => {
    onChange("system.mtproto.secrets", next);
  };

  const update = (idx: number, patch: Partial<MTProtoSecret>) => {
    const next = secrets.map((s, i) => (i === idx ? { ...s, ...patch } : s));
    commit(next);
  };

  const fetchSecret = async (): Promise<string> => {
    const sni = config.system.mtproto?.fake_sni || "storage.googleapis.com";
    try {
      const res = await fetch("/api/mtproto/generate-secret", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ fake_sni: sni }),
      });
      if (!res.ok) return "";
      const data = (await res.json()) as { success: boolean; secret?: string };
      return data.success && data.secret ? data.secret : "";
    } catch {
      return "";
    }
  };

  const add = async () => {
    setAdding(true);
    try {
      const secret = await fetchSecret().catch(() => "");
      commit([...secrets, { id: newId(), name: "", secret, enabled: true }]);
    } finally {
      setAdding(false);
    }
  };

  const remove = (idx: number) => commit(secrets.filter((_, i) => i !== idx));

  const generate = async (idx: number) => {
    setGeneratingIdx(idx);
    try {
      const secret = await fetchSecret();
      if (secret) update(idx, { secret });
    } finally {
      setGeneratingIdx(null);
    }
  };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {secrets.map((s, idx) => (
        <Paper
          key={s.id || idx}
          variant="outlined"
          sx={{
            p: 1.5,
            display: "flex",
            flexDirection: "column",
            gap: 1,
            opacity: !enabled || s.enabled ? 1 : 0.6,
          }}
        >
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            <Tooltip
              title={
                s.enabled
                  ? t("settings.MTProto.secretDisable")
                  : t("settings.MTProto.secretEnable")
              }
            >
              <Switch
                size="small"
                checked={s.enabled}
                disabled={!enabled}
                onChange={(e) => update(idx, { enabled: e.target.checked })}
              />
            </Tooltip>
            <B4TextField
              label={t("settings.MTProto.secretName")}
              value={s.name}
              onChange={(e) => update(idx, { name: e.target.value })}
              placeholder={t("settings.MTProto.secretNamePlaceholder")}
              disabled={!enabled}
              size="small"
              sx={{ flex: 1 }}
            />
            <Tooltip title={t("settings.MTProto.removeSecret")}>
              <span>
                <IconButton
                  size="small"
                  color="error"
                  disabled={!enabled}
                  onClick={() => remove(idx)}
                >
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </span>
            </Tooltip>
          </Box>
          <B4SecretField
            label={t("settings.MTProto.secret")}
            value={s.secret}
            onChange={(value) => update(idx, { secret: value })}
            placeholder={t("settings.MTProto.secretValuePlaceholder")}
            disabled={!enabled}
            helperText={t("settings.MTProto.secretHelp")}
            onGenerate={() => void generate(idx)}
            generateLabel={t("settings.MTProto.generateSecret")}
            generating={generatingIdx === idx}
            actions={
              <Tooltip title={t("settings.MTProto.shareLink")}>
                <span>
                  <IconButton
                    size="small"
                    disabled={!enabled || !s.secret}
                    onClick={() => onShare(s.secret)}
                  >
                    <IosShareIcon fontSize="small" />
                  </IconButton>
                </span>
              </Tooltip>
            }
          />
        </Paper>
      ))}

      <Button
        variant="outlined"
        size="small"
        startIcon={
          adding ? <CircularProgress size={14} /> : <AddIcon />
        }
        onClick={() => void add()}
        disabled={!enabled || adding}
        sx={{ alignSelf: "flex-start" }}
      >
        {t("settings.MTProto.addSecret")}
      </Button>
    </Box>
  );
};

export default MTProtoSecrets;

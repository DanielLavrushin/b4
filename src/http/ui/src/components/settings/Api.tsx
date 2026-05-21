import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { B4Config, AIProvider } from "@models/config";
import {
  Autocomplete,
  Box,
  Button,
  Chip,
  CircularProgress,
  DialogContent,
  Grid,
  IconButton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { RefreshIcon, ApiIcon } from "@b4.icons";
import {
  B4Alert,
  B4Dialog,
  B4FormGroup,
  B4NumberField,
  B4Section,
  B4Select,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { aiApi, AIModel } from "@api/ai";
import { useSnackbar } from "@context/SnackbarProvider";
import { useAiStatus } from "@context/AiStatusProvider";
import { colors } from "@design";

export interface ApiSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | number) => void;
}

const PROVIDERS: { value: AIProvider; label: string }[] = [
  { value: "", label: "—" },
  { value: "openai", label: "OpenAI" },
  { value: "anthropic", label: "Anthropic" },
  { value: "ollama", label: "Ollama (local)" },
];

const DEFAULT_ENDPOINTS: Record<string, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com/v1",
  ollama: "http://127.0.0.1:11434",
};

export const ApiSettings = ({ config, onChange }: ApiSettingsProps) => {
  const { t } = useTranslation();

  return (
    <Stack spacing={3}>
      <B4Alert icon={<ApiIcon />}>{t("settings.Api.alert")}</B4Alert>
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Api.ipinfoTitle")}
            description={t("settings.Api.ipinfoDescription")}
            icon={<ApiIcon />}
          >
            <B4TextField
              label={t("settings.Api.token")}
              value={config.system.api.ipinfo_token}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                onChange("system.api.ipinfo_token", e.target.value)
              }
              helperText={
                <>
                  {t("settings.Api.tokenHelp")}{" "}
                  <a
                    href="https://ipinfo.io/dashboard/token"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {t("settings.Api.tokenHelpLink")}
                  </a>
                </>
              }
              placeholder={t("settings.Api.tokenPlaceholder")}
            />
          </B4Section>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <AISection config={config} onChange={onChange} />
        </Grid>
      </Grid>
    </Stack>
  );
};

const AISection = ({ config, onChange }: ApiSettingsProps) => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();

  const ai = config.system.ai;
  const provider = ai?.provider ?? "";
  const keyRef = (ai?.api_key_ref || provider || "").trim();

  const {
    status,
    loading: statusLoading,
    refresh: refreshStatus,
  } = useAiStatus();
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [pendingKey, setPendingKey] = useState("");
  const [savingKey, setSavingKey] = useState(false);
  const [models, setModels] = useState<AIModel[]>([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState<string>("");
  const [modelsLoadedFor, setModelsLoadedFor] = useState<string>("");
  const [secretRefs, setSecretRefs] = useState<string[]>([]);

  const refreshSecretRefs = useCallback(async () => {
    try {
      const data = await aiApi.listSecrets();
      setSecretRefs(data.refs ?? []);
    } catch {
      setSecretRefs([]);
    }
  }, []);

  useEffect(() => {
    void refreshSecretRefs();
  }, [refreshSecretRefs]);

  const requiresKey = provider === "openai" || provider === "anthropic";
  const hasKey = Boolean(keyRef) && secretRefs.includes(keyRef);

  const isDirty = useMemo(() => {
    if (!status) return false;
    return (
      Boolean(ai?.enabled) !== Boolean(status.enabled) ||
      (ai?.provider ?? "") !== (status.provider ?? "") ||
      (ai?.model ?? "") !== (status.model ?? "") ||
      (ai?.endpoint ?? "") !== (status.endpoint ?? "") ||
      (ai?.api_key_ref ?? "") !== (status.api_key_ref ?? "")
    );
  }, [ai, status]);

  const readyChip = useMemo(() => {
    if (!ai?.enabled) {
      return (
        <Chip
          size="small"
          label={t("settings.Ai.statusDisabled")}
          variant="outlined"
        />
      );
    }
    if (statusLoading) {
      return (
        <Chip
          size="small"
          label={t("settings.Ai.statusChecking")}
          variant="outlined"
        />
      );
    }
    if (isDirty) {
      return (
        <Chip
          size="small"
          label={t("settings.Ai.statusUnsaved")}
          variant="outlined"
        />
      );
    }
    if (status?.ready) {
      return (
        <Chip
          size="small"
          color="success"
          label={t("settings.Ai.statusReady")}
        />
      );
    }
    return (
      <Chip
        size="small"
        color="warning"
        label={status?.not_ready_reason || t("settings.Ai.statusNotReady")}
      />
    );
  }, [ai?.enabled, status, statusLoading, isDirty, t]);

  const handleProviderChange = (value: string) => {
    onChange("system.ai.provider", value);
    onChange("system.ai.model", "");
    const next = DEFAULT_ENDPOINTS[value] ?? "";
    const current = (ai?.endpoint ?? "").trim();
    const knownDefaults = Object.values(DEFAULT_ENDPOINTS);
    const isAutoFilled = current === "" || knownDefaults.includes(current);
    if (next && isAutoFilled) {
      onChange("system.ai.endpoint", next);
    }
    setModels([]);
    setModelsError("");
    setModelsLoadedFor("");
  };

  const modelsKey = `${provider}|${(ai?.endpoint ?? "").trim()}`;

  const fetchModels = useCallback(
    async (force = false) => {
      if (!provider) return;
      if (!force && modelsLoadedFor === modelsKey && models.length > 0) return;
      try {
        setModelsLoading(true);
        setModelsError("");
        const data = await aiApi.listModels(
          provider,
          (ai?.endpoint ?? "").trim(),
        );
        const sorted = [...data.models].sort(
          (a, b) => (b.created ?? 0) - (a.created ?? 0),
        );
        setModels(sorted);
        setModelsLoadedFor(modelsKey);
      } catch (err) {
        setModels([]);
        setModelsError(
          err instanceof Error ? err.message : t("settings.Ai.modelsError"),
        );
      } finally {
        setModelsLoading(false);
      }
    },
    [provider, ai?.endpoint, modelsKey, modelsLoadedFor, models.length, t],
  );

  const openKeyDialog = () => {
    setPendingKey("");
    setKeyDialogOpen(true);
  };

  const closeKeyDialog = () => {
    if (savingKey) return;
    setKeyDialogOpen(false);
    setPendingKey("");
  };

  const saveKey = async () => {
    const ref = keyRef;
    const key = pendingKey.trim();
    if (!ref || !key) return;
    try {
      setSavingKey(true);
      await aiApi.setSecret(ref, key);
      showSuccess(t("settings.Ai.keySaved"));
      setKeyDialogOpen(false);
      setPendingKey("");
      await Promise.all([refreshStatus(), refreshSecretRefs()]);
    } catch (err) {
      showError(
        err instanceof Error ? err.message : t("settings.Ai.keySaveError"),
      );
    } finally {
      setSavingKey(false);
    }
  };

  const removeKey = async () => {
    if (!keyRef) return;
    try {
      await aiApi.deleteSecret(keyRef);
      showSuccess(t("settings.Ai.keyRemoved"));
      await Promise.all([refreshStatus(), refreshSecretRefs()]);
    } catch (err) {
      showError(
        err instanceof Error ? err.message : t("settings.Ai.keyRemoveError"),
      );
    }
  };

  return (
    <B4Section
      title={t("settings.Ai.title")}
      description={t("settings.Ai.description")}
      icon={<ApiIcon />}
    >
      <Stack spacing={2}>
        <B4Alert severity="info">{t("settings.Ai.privacyAlert")}</B4Alert>

        <B4FormGroup label={t("settings.Ai.providerSettings")} columns={2}>
          <B4Switch
            label={t("settings.Ai.enable")}
            checked={ai?.enabled ?? false}
            onChange={(checked: boolean) =>
              onChange("system.ai.enabled", checked)
            }
            description={t("settings.Ai.enableDesc")}
          />
          <B4Select
            label={t("settings.Ai.provider")}
            value={provider}
            options={PROVIDERS}
            onChange={(e) => handleProviderChange(String(e.target.value))}
            helperText={t("settings.Ai.providerHelp")}
          />
          <Autocomplete<AIModel | string, false, boolean, true>
            freeSolo
            disableClearable={!ai?.model}
            disabled={!ai?.enabled || !provider}
            options={models}
            value={ai?.model ?? ""}
            loading={modelsLoading}
            onOpen={() => {
              void fetchModels();
            }}
            onChange={(_e, newValue) => {
              if (typeof newValue === "string") {
                onChange("system.ai.model", newValue);
              } else if (newValue) {
                onChange("system.ai.model", newValue.id);
              } else {
                onChange("system.ai.model", "");
              }
            }}
            onInputChange={(_e, newInput, reason) => {
              if (reason === "input") {
                onChange("system.ai.model", newInput);
              }
            }}
            getOptionLabel={(opt) =>
              typeof opt === "string" ? opt : opt.display_name || opt.id
            }
            isOptionEqualToValue={(opt, val) => {
              const a = typeof opt === "string" ? opt : opt.id;
              const b = typeof val === "string" ? val : val.id;
              return a === b;
            }}
            renderOption={(props, opt) => {
              const id = typeof opt === "string" ? opt : opt.id;
              const label =
                typeof opt === "string" ? opt : opt.display_name || opt.id;
              return (
                <li {...props} key={id}>
                  <Stack>
                    <Typography variant="body2">{label}</Typography>
                    {label !== id && (
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {id}
                      </Typography>
                    )}
                  </Stack>
                </li>
              );
            }}
            renderInput={(params) => (
              <B4TextField
                {...params}
                label={t("settings.Ai.model")}
                placeholder={
                  provider === "openai"
                    ? "gpt-4o-mini"
                    : provider === "anthropic"
                      ? "claude-haiku-4-5"
                      : provider === "ollama"
                        ? "llama3"
                        : ""
                }
                size="small"
                helperText={modelsError || t("settings.Ai.modelHelp")}
                error={Boolean(modelsError)}
                slotProps={{
                  input: {
                    ...params.InputProps,
                    endAdornment: (
                      <>
                        {modelsLoading ? <CircularProgress size={16} /> : null}
                        {!ai?.model && (
                          <Tooltip title={t("settings.Ai.refreshModels")}>
                            <span>
                              <IconButton
                                size="small"
                                onClick={() => {
                                  void fetchModels(true);
                                }}
                                disabled={!provider || modelsLoading}
                              >
                                <RefreshIcon fontSize="small" />
                              </IconButton>
                            </span>
                          </Tooltip>
                        )}
                        {params.InputProps.endAdornment}
                      </>
                    ),
                  },
                }}
              />
            )}
          />
          <B4TextField
            label={t("settings.Ai.endpoint")}
            value={ai?.endpoint ?? ""}
            onChange={(e) => onChange("system.ai.endpoint", e.target.value)}
            placeholder={DEFAULT_ENDPOINTS[provider] ?? ""}
            disabled={!ai?.enabled || !provider}
            helperText={t("settings.Ai.endpointHelp")}
          />
        </B4FormGroup>

        {requiresKey && (
          <Box
            sx={{
              border: `1px solid ${colors.border.default}`,
              borderRadius: 1,
              p: 1.5,
            }}
          >
            <Stack
              direction={{ xs: "column", sm: "row" }}
              spacing={1.5}
              alignItems={{ xs: "stretch", sm: "center" }}
              justifyContent="space-between"
            >
              <Stack spacing={0.5}>
                <Typography
                  variant="body2"
                  sx={{ color: colors.text.primary, fontWeight: 600 }}
                >
                  {t("settings.Ai.keyTitle")}
                </Typography>
                <Stack direction="row" spacing={1} alignItems="center">
                  {hasKey ? (
                    <Chip
                      size="small"
                      color="success"
                      label={t("settings.Ai.keyConfigured")}
                    />
                  ) : (
                    <Chip
                      size="small"
                      color="warning"
                      label={t("settings.Ai.keyMissing")}
                    />
                  )}
                  {keyRef && (
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary }}
                    >
                      {t("settings.Ai.keyRefLabel", { ref: keyRef })}
                    </Typography>
                  )}
                </Stack>
              </Stack>
              <Stack direction="row" spacing={1}>
                <Button
                  size="small"
                  variant="outlined"
                  onClick={openKeyDialog}
                  disabled={!keyRef}
                >
                  {hasKey
                    ? t("settings.Ai.keyReplace")
                    : t("settings.Ai.keySet")}
                </Button>
                <Button
                  size="small"
                  variant="text"
                  color="error"
                  onClick={() => {
                    void removeKey();
                  }}
                  disabled={!hasKey}
                >
                  {t("settings.Ai.keyRemove")}
                </Button>
              </Stack>
            </Stack>
          </Box>
        )}

        <B4FormGroup label={t("settings.Ai.advanced")} columns={2}>
          <B4NumberField
            label={t("settings.Ai.maxTokens")}
            value={ai?.max_tokens ?? 1024}
            onChange={(n) => onChange("system.ai.max_tokens", n)}
            min={1}
            disabled={!ai?.enabled}
            helperText={t("settings.Ai.maxTokensHelp")}
          />
          <B4NumberField
            label={t("settings.Ai.temperature")}
            value={ai?.temperature ?? 0.2}
            onChange={(n) => onChange("system.ai.temperature", n)}
            allowDecimal
            min={0}
            max={2}
            disabled={!ai?.enabled}
            helperText={t("settings.Ai.temperatureHelp")}
          />
          <B4NumberField
            label={t("settings.Ai.timeout")}
            value={ai?.timeout_sec ?? 120}
            onChange={(n) => onChange("system.ai.timeout_sec", n)}
            min={1}
            disabled={!ai?.enabled}
            helperText={t("settings.Ai.timeoutHelp")}
          />
        </B4FormGroup>

        <Stack direction="row" alignItems="center" spacing={1}>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("settings.Ai.statusLabel")}
          </Typography>
          {readyChip}
          <Box sx={{ flex: 1 }} />
          <Button
            size="small"
            variant="text"
            onClick={() => {
              void refreshStatus();
            }}
            disabled={statusLoading}
          >
            {t("settings.Ai.refreshStatus")}
          </Button>
        </Stack>
      </Stack>

      <B4Dialog
        title={hasKey ? t("settings.Ai.keyReplace") : t("settings.Ai.keySet")}
        open={keyDialogOpen}
        onClose={closeKeyDialog}
        actions={
          <>
            <Button onClick={closeKeyDialog} disabled={savingKey}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              startIcon={savingKey ? <CircularProgress size={16} /> : undefined}
              onClick={() => {
                void saveKey();
              }}
              disabled={savingKey || !pendingKey.trim()}
            >
              {t("core.save")}
            </Button>
          </>
        }
      >
        <DialogContent sx={{ pt: 1 }}>
          <Stack spacing={1.5}>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {t("settings.Ai.keyDialogHelp", { ref: keyRef })}
            </Typography>
            <B4TextField
              label={t("settings.Ai.keyField")}
              type="password"
              value={pendingKey}
              onChange={(e) => setPendingKey(e.target.value)}
              placeholder={
                provider === "openai"
                  ? "sk-..."
                  : provider === "anthropic"
                    ? "sk-ant-..."
                    : ""
              }
              autoComplete="new-password"
            />
          </Stack>
        </DialogContent>
      </B4Dialog>
    </B4Section>
  );
};

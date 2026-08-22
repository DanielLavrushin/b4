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
  Switch,
  Tooltip,
  Typography,
} from "@mui/material";
import { RefreshIcon, AiIcon, IpInfoIcon, McpIcon } from "@b4.icons";
import {
  B4Accordion,
  B4Alert,
  B4ConnectDetails,
  B4Dialog,
  B4IntegrationCard,
  B4NumberField,
  B4SecretField,
  B4Tab,
  B4Tabs,
  B4TextField,
} from "@b4.elements";
import { aiApi, mcpApi, AIModel } from "@api/ai";
import { useSnackbar } from "@context/SnackbarProvider";
import { useAiStatus } from "@context/AiStatusProvider";
import { colors } from "@design";

export interface ApiSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | number) => void;
}

interface ProviderOption {
  value: AIProvider;
  label: string;
  cloud: boolean;
}

const PROVIDERS: ProviderOption[] = [
  { value: "openai", label: "OpenAI", cloud: true },
  { value: "anthropic", label: "Anthropic", cloud: true },
  { value: "ollama", label: "Ollama", cloud: false },
  { value: "openai-compatible", label: "OpenAI-compatible", cloud: false },
];

const DEFAULT_ENDPOINTS: Record<string, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com/v1",
  ollama: "http://127.0.0.1:11434",
  "openai-compatible": "http://127.0.0.1:1234/v1",
};

const MODEL_PLACEHOLDERS: Record<string, string> = {
  openai: "gpt-4o-mini",
  anthropic: "claude-haiku-4-5",
  ollama: "llama3",
  "openai-compatible": "qwen3-8b-instruct",
};

export const ApiSettings = ({ config, onChange }: ApiSettingsProps) => (
  <Stack spacing={2}>
    <IPInfoCard config={config} onChange={onChange} />
    <AICard config={config} onChange={onChange} />
    <MCPCard config={config} onChange={onChange} />
  </Stack>
);

const IPInfoCard = ({ config, onChange }: ApiSettingsProps) => {
  const { t } = useTranslation();
  const token = config.system.api.ipinfo_token ?? "";

  return (
    <B4IntegrationCard
      icon={<IpInfoIcon />}
      title={t("settings.Api.ipinfoTitle")}
      description={t("settings.Api.ipinfoDescription")}
    >
      <Box sx={{ maxWidth: 560 }}>
        <B4SecretField
          label={t("settings.Api.token")}
          value={token}
          onChange={(value) => onChange("system.api.ipinfo_token", value)}
          placeholder={t("settings.Api.tokenPlaceholder")}
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
        />
      </Box>
    </B4IntegrationCard>
  );
};

const AICard = ({ config, onChange }: ApiSettingsProps) => {
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
  const supportsKey = requiresKey || provider === "openai-compatible";
  const hasKey = Boolean(keyRef) && secretRefs.includes(keyRef);
  const selected = PROVIDERS.find((p) => p.value === provider);
  const providerIndex = PROVIDERS.findIndex((p) => p.value === provider);

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

  const notReady = useMemo(() => {
    if (!ai?.enabled || statusLoading || isDirty || !status || status.ready) {
      return null;
    }
    return (
      <Tooltip title={t("settings.Ai.refreshStatus")}>
        <Chip
          size="small"
          color="warning"
          label={status.not_ready_reason ?? t("settings.Ai.statusNotReady")}
          onClick={() => {
            void refreshStatus();
          }}
          sx={{ maxWidth: 280 }}
        />
      </Tooltip>
    );
  }, [ai?.enabled, status, statusLoading, isDirty, refreshStatus, t]);

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
    <B4IntegrationCard
      icon={<AiIcon />}
      title={t("settings.Ai.title")}
      description={t("settings.Ai.description")}
      status={notReady}
      enabled={ai?.enabled ?? false}
      onToggle={(checked) => onChange("system.ai.enabled", checked)}
      toggleLabel={t("settings.Ai.enable")}
    >
      <Stack spacing={2}>
        <B4Tabs
          value={providerIndex >= 0 ? providerIndex : false}
          onChange={(_, index: number) =>
            handleProviderChange(PROVIDERS[index].value)
          }
        >
          {PROVIDERS.map((p) => (
            <B4Tab key={p.value} label={p.label} />
          ))}
        </B4Tabs>
        {selected?.cloud && (
          <B4Alert severity="info">
            {t("settings.Ai.privacyCloud", { provider: selected.label })}
          </B4Alert>
        )}
      </Stack>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Autocomplete<AIModel | string, false, boolean, true>
            freeSolo
            disableClearable={!ai?.model}
            disabled={!provider}
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
                placeholder={MODEL_PLACEHOLDERS[provider] ?? ""}
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
        </Grid>

        {supportsKey && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4SecretField
              label={t("settings.Ai.keyTitle")}
              managed
              configured={hasKey}
              disabled={!keyRef}
              placeholder={
                requiresKey
                  ? t("settings.Ai.keyMissing")
                  : t("settings.Ai.keyOptional")
              }
              helperText={t("settings.Ai.keyStoredHelp")}
              caption={
                keyRef ? t("settings.Ai.keyRefLabel", { ref: keyRef }) : null
              }
              onSet={openKeyDialog}
              setLabel={
                hasKey ? t("settings.Ai.keyReplace") : t("settings.Ai.keySet")
              }
              onRemove={() => {
                void removeKey();
              }}
              removeLabel={t("settings.Ai.keyRemove")}
            />
          </Grid>
        )}
      </Grid>

      <B4Accordion title={t("settings.Ai.advanced")}>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12 }}>
            <B4TextField
              label={t("settings.Ai.endpoint")}
              value={ai?.endpoint ?? ""}
              onChange={(e) => onChange("system.ai.endpoint", e.target.value)}
              placeholder={DEFAULT_ENDPOINTS[provider] ?? ""}
              disabled={!provider}
              helperText={t("settings.Ai.endpointHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.Ai.maxTokens")}
              value={ai?.max_tokens ?? 1024}
              onChange={(n) => onChange("system.ai.max_tokens", n)}
              min={1}
              helperText={t("settings.Ai.maxTokensHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.Ai.temperature")}
              value={ai?.temperature ?? 0.2}
              onChange={(n) => onChange("system.ai.temperature", n)}
              allowDecimal
              min={0}
              max={2}
              helperText={t("settings.Ai.temperatureHelp")}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4NumberField
              label={t("settings.Ai.timeout")}
              value={ai?.timeout_sec ?? 120}
              onChange={(n) => onChange("system.ai.timeout_sec", n)}
              min={1}
              helperText={t("settings.Ai.timeoutHelp")}
            />
          </Grid>
        </Grid>
      </B4Accordion>

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
    </B4IntegrationCard>
  );
};

const shortenToken = (token: string) =>
  token.length > 24 ? `${token.slice(0, 8)}…${token.slice(-6)}` : token;

const clientSnippet = (url: string, token: string) =>
  [
    `"b4": {`,
    `  "url": "${url}",`,
    `  "headers": { "Authorization": "Bearer ${token}" }`,
    `}`,
  ].join("\n");

const MCPCard = ({ config, onChange }: ApiSettingsProps) => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();

  const mcp = config.system.web_server.mcp;
  const enabled = Boolean(mcp?.enabled);
  const allowWrites = Boolean(mcp?.allow_writes);
  const token = mcp?.token ?? "";
  const [generating, setGenerating] = useState(false);

  const generateToken = async () => {
    try {
      setGenerating(true);
      const data = await mcpApi.generateToken();
      onChange("system.web_server.mcp.token", data.token);
      showSuccess(t("settings.Mcp.tokenGenerated"));
    } catch {
      showError(t("settings.Mcp.tokenGenerateError"));
    } finally {
      setGenerating(false);
    }
  };

  const authConfigured = Boolean(
    config.system.web_server.username &&
      (config.system.web_server.password ||
        config.system.web_server.password_set),
  );

  const endpointUrl = `${window.location.origin}/api/mcp`;
  const placeholderToken = t("settings.Mcp.tokenSnippetPlaceholder");

  const notice = (() => {
    if (token) return null;
    if (!authConfigured) {
      return (
        <B4Alert severity="warning">{t("settings.Mcp.noAuthWarning")}</B4Alert>
      );
    }
    return <B4Alert severity="info">{t("settings.Mcp.noTokenWarning")}</B4Alert>;
  })();

  return (
    <B4IntegrationCard
      icon={<McpIcon />}
      title={t("settings.Mcp.title")}
      description={t("settings.Mcp.description")}
      enabled={enabled}
      onToggle={(checked) => onChange("system.web_server.mcp.enabled", checked)}
      toggleLabel={t("settings.Mcp.enabled")}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <Stack spacing={2}>
            <B4SecretField
              label={t("settings.Mcp.token")}
              value={token}
              onChange={(value) =>
                onChange("system.web_server.mcp.token", value)
              }
              placeholder={t("settings.Mcp.tokenPlaceholder")}
              helperText={t("settings.Mcp.tokenHelp")}
              onGenerate={() => {
                void generateToken();
              }}
              generateLabel={
                token ? t("settings.Mcp.tokenRegenerate") : t("core.generate")
              }
              generating={generating}
            />

            <Box
              sx={{
                display: "flex",
                alignItems: "center",
                gap: 2,
                p: "13px 15px",
                borderRadius: "6px",
                border: `1px solid ${allowWrites ? "rgba(255, 167, 38, 0.32)" : colors.border.light}`,
                borderLeft: `3px solid ${allowWrites ? colors.state.warning : colors.border.medium}`,
                bgcolor: allowWrites
                  ? "rgba(255, 167, 38, 0.06)"
                  : colors.background.hover,
              }}
            >
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography sx={{ color: colors.text.primary, fontWeight: 500 }}>
                  {t("settings.Mcp.allowWrites")}
                </Typography>
                <Typography
                  variant="caption"
                  sx={{
                    display: "block",
                    color: colors.text.secondary,
                    mt: "2px",
                  }}
                >
                  {allowWrites
                    ? t("settings.Mcp.allowWritesWarning")
                    : t("settings.Mcp.allowWritesHelp")}
                </Typography>
              </Box>
              <Switch
                checked={allowWrites}
                onChange={(e) =>
                  onChange(
                    "system.web_server.mcp.allow_writes",
                    e.target.checked,
                  )
                }
                slotProps={{
                  input: { "aria-label": t("settings.Mcp.allowWrites") },
                }}
              />
            </Box>
          </Stack>
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4ConnectDetails
            label={t("settings.Mcp.clientConfig")}
            snippet={clientSnippet(
              endpointUrl,
              token ? shortenToken(token) : placeholderToken,
            )}
            copyValue={clientSnippet(endpointUrl, token || placeholderToken)}
            copiedMessage={t("settings.Mcp.clientConfigCopied")}
            footer={notice}
          />
        </Grid>
      </Grid>
    </B4IntegrationCard>
  );
};

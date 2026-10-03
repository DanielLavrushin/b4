import {
  Backdrop,
  Box,
  Button,
  Chip,
  CircularProgress,
  Container,
  DialogContent,
  DialogContentText,
  Fade,
  Grid,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import i18n, { setLanguage } from "../../i18n";

import {
  ApiIcon,
  BackupIcon,
  CaptureIcon,
  CoreIcon,
  DiscoveryIcon,
  DomainIcon,
  RefreshIcon,
  SaveIcon,
  TelegramIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { useAiStatus } from "@context/AiStatusProvider";
import { useHubInvalidate } from "@hooks/useHub";
import { useTelegramBridgeInvalidate } from "@hooks/useTelegramBridge";
import { useSystemAddressesInvalidate } from "@hooks/useSystemAddresses";
import { ApiSettings } from "./Api";
import { CaptureSettings } from "./Capture";
import { CheckerSettings } from "./Discovery";
import { GeoSettings } from "./Geo";
import { MTProtoSettings } from "./telegram/MTProto";
import { BackupSettings } from "./Backup";
import { CoreSettings } from "./CoreSettings";
import { RestartDialog } from "./RestartDialog";
import {
  CORE_SECTIONS,
  CoreSectionId,
  coreSectionIndex,
} from "./coreSections";

import { B4Alert, B4Dialog, B4Tab, B4Tabs } from "@b4.elements";
import { configApi, SettingsPropHandlerType } from "@b4.settings";
import { isStaleWriteError, reportSaveError, reportStaleWrite } from "@utils";
import { colors, spacing } from "@design";

import { B4Config } from "@models/config";

const changed = (pick: (c: B4Config) => unknown, a: B4Config, b: B4Config) =>
  JSON.stringify(pick(a)) !== JSON.stringify(pick(b));

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

function TabPanel({
  children,
  value,
  index,
  ...other
}: Readonly<TabPanelProps>) {
  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      id={`settings-tabpanel-${index}`}
      aria-labelledby={`settings-tab-${index}`}
      {...other}
    >
      {value === index && (
        <Fade in>{<Box sx={{ pt: 3 }}>{children}</Box>}</Fade>
      )}
    </div>
  );
}

enum TABS {
  GENERAL = 0,
  DOMAINS,
  DISCOVERY,
  MTPROTO,
  API,
  PAYLOADS,
  BACKUP,
}

export function SettingsPage() {
  const { showError, showSuccess, showSnackbar } = useSnackbar();
  const { t } = useTranslation();
  const [config, setConfig] = useState<B4Config | null>(null);
  const [originalConfig, setOriginalConfig] = useState<B4Config | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showResetDialog, setShowResetDialog] = useState(false);
  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);
  const lastCoreSection = useRef<CoreSectionId>(CORE_SECTIONS[0].id);

  const navigate = useNavigate();
  const location = useLocation();

  // Settings categories with route paths
  const settingCategories = useMemo(
    () => [
      {
        id: TABS.GENERAL,
        path: "general",
        label: t("settings.tabs.core"),
        icon: <CoreIcon />,
        description: t("settings.tabs.coreDesc"),
        requiresRestart: true,
      },
      {
        id: TABS.DOMAINS,
        path: "domains",
        label: t("settings.tabs.geodat"),
        icon: <DomainIcon />,
        description: t("settings.tabs.geodatDesc"),
        requiresRestart: false,
      },
      {
        id: TABS.DISCOVERY,
        path: "discovery",
        label: t("settings.tabs.discovery"),
        icon: <DiscoveryIcon />,
        description: t("settings.tabs.discoveryDesc"),
        requiresRestart: false,
      },
      {
        id: TABS.MTPROTO,
        path: "mtproto",
        label: t("settings.tabs.mtproto"),
        icon: <TelegramIcon />,
        description: t("settings.tabs.mtprotoDesc"),
        requiresRestart: false,
      },
      {
        id: TABS.API,
        path: "api",
        label: t("settings.tabs.api"),
        icon: <ApiIcon />,
        description: t("settings.tabs.apiDesc"),
        requiresRestart: false,
      },
      {
        id: TABS.PAYLOADS,
        path: "payloads",
        label: t("settings.tabs.payloads"),
        icon: <CaptureIcon />,
        description: t("settings.tabs.payloadsDesc"),
        requiresRestart: false,
      },
      {
        id: TABS.BACKUP,
        path: "backup",
        label: t("settings.tabs.backup"),
        icon: <BackupIcon />,
        description: t("settings.tabs.backupDesc"),
        requiresRestart: false,
      },
    ],
    [t],
  );

  // Determine current tab based on URL
  const [currentTabPath, currentSectionPath] = (
    location.pathname.split("/settings/")[1] ?? ""
  ).split("/");
  const currentTab =
    settingCategories.find((cat) => cat.path === (currentTabPath || "general"))
      ?.id ?? TABS.GENERAL;
  const currentSectionIndex = coreSectionIndex(currentSectionPath);
  const currentSection = CORE_SECTIONS[currentSectionIndex].id;

  useEffect(() => {
    if (currentTab === TABS.GENERAL) {
      lastCoreSection.current = currentSection;
    }
  }, [currentTab, currentSection]);

  // Handle tab change
  const handleTabChange = (_: React.SyntheticEvent, newValue: TABS) => {
    const category = settingCategories.find((cat) => cat.id === newValue);
    if (category) {
      const path =
        category.id === TABS.GENERAL
          ? `${category.path}/${lastCoreSection.current}`
          : category.path;
      navigate(`/settings/${path}`)?.catch(() => {});
    }
  };

  const handleSectionChange = (_: React.SyntheticEvent, index: number) => {
    const section = CORE_SECTIONS[index];
    if (section) {
      navigate(`/settings/general/${section.id}`)?.catch(() => {});
    }
  };

  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 });
  }, [location.pathname]);

  // Navigate to default tab if no specific tab is in URL
  useEffect(() => {
    if (
      location.pathname === "/settings" ||
      location.pathname === "/settings/"
    ) {
      navigate("/settings/general", { replace: true })?.catch(() => {});
    }
  }, [location.pathname, navigate]);

  // Check if configuration has been modified
  const hasChanges = useMemo(() => {
    if (!config || !originalConfig) return false;
    return JSON.stringify(config) !== JSON.stringify(originalConfig);
  }, [config, originalConfig]);

  const coreSectionState = useMemo(
    () =>
      CORE_SECTIONS.map((section) => {
        if (!hasChanges || !config || !originalConfig) {
          return { dirty: false, restart: false };
        }
        const dirty = changed(section.pick, config, originalConfig);
        return {
          dirty,
          restart: dirty && changed(section.restartPick, config, originalConfig),
        };
      }),
    [config, originalConfig, hasChanges],
  );

  // Check which categories have changes
  const categoryHasChanges = useMemo(() => {
    if (!hasChanges || !config || !originalConfig) return {};

    return {
      // Core
      [TABS.GENERAL]: coreSectionState.some((s) => s.dirty),

      // Geosite Settings
      [TABS.DOMAINS]:
        JSON.stringify(config.system.geo) !==
        JSON.stringify(originalConfig.system.geo),

      // Discovery
      [TABS.DISCOVERY]:
        JSON.stringify(config.system.checker) !==
        JSON.stringify(originalConfig.system.checker),

      // MTProto
      [TABS.MTPROTO]:
        JSON.stringify(config.system.mtproto) !==
        JSON.stringify(originalConfig.system.mtproto),

      // API
      [TABS.API]:
        JSON.stringify(config.system.api) !==
          JSON.stringify(originalConfig.system.api) ||
        JSON.stringify(config.system.ai) !==
          JSON.stringify(originalConfig.system.ai) ||
        JSON.stringify(config.system.web_server.mcp) !==
          JSON.stringify(originalConfig.system.web_server.mcp) ||
        JSON.stringify(config.system.hub) !==
          JSON.stringify(originalConfig.system.hub),

      // PAYLOADS
      [TABS.PAYLOADS]: false,

      // Backup
      [TABS.BACKUP]: false,
    };
  }, [config, originalConfig, hasChanges, coreSectionState]);

  const generalNeedsRestart = coreSectionState.some((s) => s.restart);

  const showErrorRef = useRef(showError);
  showErrorRef.current = showError;

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      const data = await configApi.get();
      setConfig(data);
      setOriginalConfig(structuredClone(data));
      setLanguage(data.system.web_server.language ?? "en");
      return data;
    } catch (error) {
      console.error("Error loading configuration:", error);
      showErrorRef.current(i18n.t("core.configLoadError"));
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadConfig().catch(() => {});
  }, [loadConfig]);

  const { refresh: refreshAiStatus } = useAiStatus();
  const invalidateHub = useHubInvalidate();
  const invalidateTelegramBridge = useTelegramBridgeInvalidate();
  const invalidateSystemAddresses = useSystemAddressesInvalidate();

  const saveConfig = async () => {
    if (!config) return;

    let stale = false;
    try {
      setSaving(true);
      await configApi.save(config);
      setOriginalConfig(structuredClone(config));

      if (generalNeedsRestart) {
        showSuccess(t("core.configSavedRestart"), {
          label: t("settings.RestartDialog.restartButton"),
          onClick: () => setShowRestartDialog(true),
        });
      } else {
        showSuccess(t("core.configSaved"));
      }
    } catch (error) {
      if (isStaleWriteError(error)) {
        stale = true;
        reportStaleWrite(error, showSnackbar, t, () => {
          loadConfig().catch(() => {});
        });
      } else {
        reportSaveError(error, showError, t);
      }
    } finally {
      setSaving(false);
      if (!stale) {
        await loadConfig();
        void refreshAiStatus();
        void invalidateHub();
        void invalidateTelegramBridge();
        void invalidateSystemAddresses();
      }
    }
  };

  const resetChanges = () => {
    if (originalConfig) {
      setConfig(structuredClone(originalConfig));
      setLanguage(originalConfig.system.web_server.language ?? "en");
      setShowResetDialog(false);
      showSuccess(t("core.changesDiscarded"));
    }
  };

  const handleChange = (field: string, value: SettingsPropHandlerType) => {
    setConfig((prev) => {
      if (!prev) return prev;

      const keys = field.split(".");

      if (keys.length === 1) {
        return { ...prev, [field]: value };
      }

      const newConfig = { ...prev };
      let current: Record<string, unknown> = newConfig;

      for (let i = 0; i < keys.length - 1; i++) {
        current[keys[i]] = { ...(current[keys[i]] as object) };
        current = current[keys[i]] as Record<string, unknown>;
      }

      current[keys.at(-1)!] = value;
      return newConfig;
    });
  };

  if (loading || !config) {
    return (
      <Backdrop open sx={{ zIndex: 9999 }}>
        <Stack alignItems="center" spacing={2}>
          <CircularProgress sx={{ color: colors.secondary }} />
          <Typography sx={{ color: colors.text.primary }}>
            {t("core.loadingConfiguration")}
          </Typography>
        </Stack>
      </Backdrop>
    );
  }

  const validTab = Math.max(currentTab, 0);

  return (
    <Container
      maxWidth={false}
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        py: 3,
      }}
    >
      {/* Header with tabs */}
      <Paper
        elevation={0}
        sx={{
          bgcolor: colors.background.paper,
          borderRadius: 2,
          border: `1px solid ${colors.border.default}`,
        }}
      >
        <Box sx={{ p: 2, pb: 0 }}>
          {/* Action bar */}
          <Stack
            direction="row"
            justifyContent="space-between"
            alignItems="center"
            flexWrap="wrap"
            useFlexGap
            spacing={1}
            sx={{ mb: 2 }}
          >
            <Stack direction="row" spacing={2} alignItems="center">
              <Typography
                sx={{
                  color: colors.text.primary,
                  fontSize: 18,
                  fontWeight: 600,
                  lineHeight: 1.3,
                }}
              >
                {t("core.configuration")}
              </Typography>
              {hasChanges && (
                <Chip
                  label={t("core.modified")}
                  size="small"
                  icon={<WarningIcon />}
                  color="secondary"
                  variant="outlined"
                />
              )}
            </Stack>

            <Stack
              direction="row"
              spacing={1}
              alignItems="center"
              justifyContent="flex-end"
              flexWrap="wrap"
              useFlexGap
              sx={{ flex: { xs: "1 1 100%", sm: "0 1 auto" } }}
            >
              {generalNeedsRestart && (
                <B4Alert severity="warning" sx={{ py: 0, px: spacing.sm }}>
                  <Trans
                    i18nKey="core.coreRestartWarning"
                    components={{ strong: <strong /> }}
                  />
                </B4Alert>
              )}
              <Button
                size="small"
                variant="text"
                onClick={() => setShowResetDialog(true)}
                disabled={!hasChanges || saving}
              >
                {t("core.discard")}
              </Button>
              <Button
                size="small"
                variant="outlined"
                startIcon={<RefreshIcon />}
                onClick={() => {
                  loadConfig().catch(() => {});
                }}
                disabled={saving}
                sx={{ display: { xs: "none", sm: "inline-flex" } }}
              >
                {t("core.reload")}
              </Button>

              <Button
                size="small"
                variant="contained"
                startIcon={
                  saving ? <CircularProgress size={16} /> : <SaveIcon />
                }
                onClick={() => {
                  void saveConfig();
                }}
                disabled={!hasChanges || saving}
              >
                {saving ? t("core.saving") : t("core.save")}
              </Button>
            </Stack>
          </Stack>

          {/* Tabs */}
          <B4Tabs value={validTab} onChange={handleTabChange}>
            {[...settingCategories]
              .sort((a, b) => a.id - b.id)
              .map((cat) => (
                <B4Tab
                  key={cat.id}
                  icon={cat.icon}
                  label={cat.label}
                  inline
                  hasChanges={categoryHasChanges[cat.id]}
                />
              ))}
          </B4Tabs>
          {currentTab === TABS.GENERAL && (
            <B4Tabs
              value={currentSectionIndex}
              onChange={handleSectionChange}
              sx={{
                borderBottom: "none",
                "& .MuiTab-icon": {
                  display: { xs: "none", sm: "inline-flex" },
                },
              }}
            >
              {CORE_SECTIONS.map((section, index) => (
                <B4Tab
                  key={section.id}
                  icon={<section.Icon />}
                  label={t(section.labelKey)}
                  inline
                  index={index}
                  idPrefix="core-section"
                  hasChanges={coreSectionState[index].dirty}
                  needsRestart={coreSectionState[index].restart}
                  needsRestartLabel={t("settings.coreTabs.needsRestart")}
                />
              ))}
            </B4Tabs>
          )}
        </Box>
      </Paper>

      <Box ref={contentRef} sx={{ flex: 1, overflow: "auto", pb: 2 }}>
        <TabPanel value={validTab} index={TABS.GENERAL}>
          <CoreSettings
            section={currentSection}
            config={config}
            onChange={handleChange}
          />
        </TabPanel>

        <TabPanel value={validTab} index={TABS.DOMAINS}>
          <GeoSettings
            config={config}
            onChange={handleChange}
            loadConfig={() => {
              loadConfig().catch(() => {});
            }}
          />
        </TabPanel>

        <TabPanel value={validTab} index={TABS.API}>
          <ApiSettings config={config} onChange={handleChange} />
        </TabPanel>

        <TabPanel value={validTab} index={TABS.DISCOVERY}>
          <CheckerSettings config={config} onChange={handleChange} />
        </TabPanel>

        <TabPanel value={validTab} index={TABS.MTPROTO}>
          <Grid container spacing={spacing.lg} alignItems="stretch">
            <Grid size={{ xs: 12 }} sx={{ display: "flex" }}>
              <Box sx={{ width: "100%" }}>
                <MTProtoSettings
                  config={config}
                  savedMtproto={originalConfig?.system.mtproto}
                  savedBridgeEnabled={
                    originalConfig?.system.mtproto?.bridge?.enabled ?? false
                  }
                  onChange={handleChange}
                />
              </Box>
            </Grid>
          </Grid>
        </TabPanel>

        <TabPanel value={validTab} index={TABS.PAYLOADS}>
          <CaptureSettings />
        </TabPanel>

        <TabPanel value={validTab} index={TABS.BACKUP}>
          <BackupSettings />
        </TabPanel>
      </Box>

      {/* Reset Confirmation Dialog */}
      <B4Dialog
        title={t("core.discardChanges")}
        open={showResetDialog}
        onClose={() => setShowResetDialog(false)}
        actions={
          <>
            <Button onClick={() => setShowResetDialog(false)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button onClick={resetChanges} variant="contained">
              {t("core.discard")}
            </Button>
          </>
        }
      >
        <DialogContent>
          <DialogContentText>{t("core.discardConfirm")}</DialogContentText>
        </DialogContent>
      </B4Dialog>

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
    </Container>
  );
}

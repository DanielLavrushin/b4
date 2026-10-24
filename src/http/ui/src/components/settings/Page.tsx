import {
  Backdrop,
  Box,
  Button,
  Chip,
  CircularProgress,
  Container,
  Fade,
  Grid,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, type BlockerFunction } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import i18n, { setLanguage } from "../../i18n";

import {
  ApiIcon,
  CaptureIcon,
  CoreIcon,
  DiscoveryIcon,
  DomainIcon,
  RefreshIcon,
  SaveIcon,
  SystemIcon,
  TelegramIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { useAiStatus } from "@context/AiStatusProvider";
import { useHubInvalidate } from "@hooks/useHub";
import { usePageTitleDetail } from "@hooks/usePageTitle";
import { useTelegramBridgeInvalidate } from "@hooks/useTelegramBridge";
import { useSystemAddressesInvalidate } from "@hooks/useSystemAddresses";
import { ApiSettings } from "./Api";
import { CaptureSettings } from "./Capture";
import { CheckerSettings } from "./Discovery";
import { GeoSettings } from "./Geo";
import { MTProtoSettings } from "./telegram/MTProto";
import { CoreSettings } from "./CoreSettings";
import { SystemSettings } from "./SystemSettings";
import { RestartDialog } from "./RestartDialog";
import {
  CORE_SECTIONS,
  SYSTEM_SECTIONS,
  SETTINGS_DIALOG_SUB_DEFS,
  SettingsSection,
  sectionIndex,
} from "./sections";

import { B4Alert, B4Tab, B4Tabs } from "@b4.elements";
import { configApi, SettingsPropHandlerType } from "@b4.settings";
import {
  isStaleWriteError,
  reportSaveError,
  reportStaleWrite,
  setByPath,
} from "@utils";
import {
  ChangedFieldsProvider,
} from "@context/ChangedFieldsContext";
import { useUnsavedChangesGuard } from "@hooks/useUnsavedChangesGuard";
import {
  UnsavedChangesDialog,
  type ChangeGroupDef,
} from "@common/UnsavedChangesDialog";
import {
  useChangeItems,
  useChangeTracker,
} from "@common/useChangeDialog";
import { colors, spacing } from "@design";
import { B4Config } from "@models/config";
const SETTINGS_DIFF_IGNORE = new Set(["sets", "available_ifaces", "revision"]);
const changed = (pick: (c: B4Config) => unknown, a: B4Config, b: B4Config) =>
  JSON.stringify(pick(a)) !== JSON.stringify(pick(b));
const changedPrefix = (
  paths: ReadonlySet<string>,
  prefix: string,
): boolean => {
  for (const p of paths) {
    if (p === prefix || p.startsWith(`${prefix}.`)) return true;
  }
  return false;
};
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
  SYSTEM,
  DOMAINS,
  DISCOVERY,
  MTPROTO,
  API,
  PAYLOADS,
}

const TAB_SECTIONS = new Map<TABS, SettingsSection[]>([
  [TABS.GENERAL, CORE_SECTIONS],
  [TABS.SYSTEM, SYSTEM_SECTIONS],
]);

const TAB_GROUP_PREFIXES: Record<TABS, { labelKey: string; prefixes: string[] }> = {
  [TABS.GENERAL]: { labelKey: "settings.tabs.core", prefixes: ["queue", "system.tables", "system.dns", "system.socks5", "system.ip_health"] },
  [TABS.SYSTEM]: { labelKey: "settings.tabs.system", prefixes: ["system.logging", "system.memory_limit", "system.timezone", "system.update", "system.web_server"] },
  [TABS.DOMAINS]: { labelKey: "settings.tabs.geodat", prefixes: ["system.geo"] },
  [TABS.DISCOVERY]: { labelKey: "settings.tabs.discovery", prefixes: ["system.checker"] },
  [TABS.MTPROTO]: { labelKey: "settings.tabs.mtproto", prefixes: ["system.mtproto"] },
  [TABS.API]: { labelKey: "settings.tabs.api", prefixes: ["system.api", "system.ai", "system.web_server.mcp", "system.hub"] },
  [TABS.PAYLOADS]: { labelKey: "settings.tabs.payloads", prefixes: [] },
};
const SETTINGS_TAB_GROUPS: ChangeGroupDef[] = (
  Object.values(TAB_GROUP_PREFIXES) as { labelKey: string; prefixes: string[] }[]
)
  .filter((g) => g.prefixes.length > 0)
  .map(({ labelKey, prefixes }) => ({ labelKey, prefixes }));

interface SectionState {
  dirty: boolean;
  restart: boolean;
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
  const lastSection = useRef<Partial<Record<TABS, string>>>({});

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
      },
      {
        id: TABS.SYSTEM,
        path: "system",
        label: t("settings.tabs.system"),
        icon: <SystemIcon />,
      },
      {
        id: TABS.DOMAINS,
        path: "domains",
        label: t("settings.tabs.geodat"),
        icon: <DomainIcon />,
      },
      {
        id: TABS.DISCOVERY,
        path: "discovery",
        label: t("settings.tabs.discovery"),
        icon: <DiscoveryIcon />,
      },
      {
        id: TABS.MTPROTO,
        path: "mtproto",
        label: t("settings.tabs.mtproto"),
        icon: <TelegramIcon />,
      },
      {
        id: TABS.API,
        path: "api",
        label: t("settings.tabs.api"),
        icon: <ApiIcon />,
      },
      {
        id: TABS.PAYLOADS,
        path: "payloads",
        label: t("settings.tabs.payloads"),
        icon: <CaptureIcon />,
      },
    ],
    [t],
  );

  // Determine current tab based on URL
  const [currentTabPath, currentSectionPath] = (
    location.pathname.split("/settings/")[1] ?? ""
  ).split("/");
  const currentCategory =
    settingCategories.find(
      (cat) => cat.path === (currentTabPath || "general"),
    ) ?? settingCategories[0];
  const currentTab = currentCategory.id;
  const currentSections = TAB_SECTIONS.get(currentTab);
  const currentSectionIndex = currentSections
    ? sectionIndex(currentSections, currentSectionPath)
    : 0;
  const currentSectionId = currentSections?.[currentSectionIndex].id;

  useEffect(() => {
    if (currentSectionId) {
      lastSection.current[currentTab] = currentSectionId;
    }
  }, [currentTab, currentSectionId]);

  usePageTitleDetail(currentCategory.label);

  // Handle tab change
  const handleTabChange = (_: React.SyntheticEvent, newValue: TABS) => {
    const category = settingCategories.find((cat) => cat.id === newValue);
    if (category) {
      const sections = TAB_SECTIONS.get(category.id);
      const path = sections
        ? `${category.path}/${lastSection.current[category.id] ?? sections[0].id}`
        : category.path;
      navigate(`/settings/${path}`)?.catch(() => {});
    }
  };

  const handleSectionChange = (_: React.SyntheticEvent, index: number) => {
    const section = currentSections?.[index];
    if (section) {
      navigate(`/settings/${currentCategory.path}/${section.id}`)?.catch(
        () => {},
      );
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
    } else if (currentTabPath === "backup") {
      navigate("/settings/system/backup", { replace: true })?.catch(() => {});
    }
  }, [location.pathname, currentTabPath, navigate]);

  const deferredConfig = useDeferredValue(config);
  const {
    changedLeaves,
    changedPaths,
    providerProps: changeProviderProps,
  } = useChangeTracker({
    draft: deferredConfig as unknown as Record<string, unknown> | null,
    baseline: originalConfig as unknown as Record<string, unknown> | null,
    ignoredRootKeys: SETTINGS_DIFF_IGNORE,
    scope: "settings",
  });
  const draftSets = deferredConfig?.sets;
  const baselineSets = originalConfig?.sets;
  const setsChanged = useMemo(
    () => JSON.stringify(draftSets) !== JSON.stringify(baselineSets),
    [draftSets, baselineSets],
  );
  const hasChanges = changedLeaves.length > 0 || setsChanged;
  const hasUnsavedChanges = hasChanges || config !== deferredConfig;
  const shouldBlock = useCallback<BlockerFunction>(
    ({ nextLocation }) =>
      hasUnsavedChanges && !nextLocation.pathname.startsWith("/settings"),
    [hasUnsavedChanges],
  );
  const blocker = useUnsavedChangesGuard(shouldBlock, hasUnsavedChanges);
  const dialogsOpen = showResetDialog || blocker.state === "blocked";

  const settingsSubDefs = useMemo(
    () => [
      ...CORE_SECTIONS.filter((s) => s.prefixes.length > 0).map((s) => ({
        prefixes: s.prefixes,
        label: t(s.labelKey),
        blocks: s.blocks?.map((b) => ({ prefixes: b.prefixes, label: t(b.labelKey) })),
      })),
      ...SYSTEM_SECTIONS.filter((s) => s.prefixes.length > 0).map((s) => ({
        prefixes: s.prefixes,
        label: t(s.labelKey),
        blocks: s.blocks?.map((b) => ({ prefixes: b.prefixes, label: t(b.labelKey) })),
      })),
      ...SETTINGS_DIALOG_SUB_DEFS.map((s) => ({
        prefixes: s.prefixes,
        label: t(s.labelKey),
        blocks: s.blocks?.map((b) => ({ prefixes: b.prefixes, label: t(b.labelKey) })),
      })),
    ],
    [t],
  );
  const { changeItems, changeGroups } = useChangeItems({
    changedLeaves,
    registry: changeProviderProps.registry,
    valueRegistry: changeProviderProps.valueRegistry,
    groups: SETTINGS_TAB_GROUPS,
    subDefs: settingsSubDefs,
    dialogsOpen,
  });

  const sectionState = useMemo(() => {
    const state: Partial<Record<TABS, SectionState[]>> = {};
    for (const [tab, sections] of TAB_SECTIONS) {
      state[tab] = sections.map((section) => {
        if (!hasChanges || !deferredConfig || !originalConfig) {
          return { dirty: false, restart: false };
        }
        let dirty = false;
        for (const p of changedPaths) {
          if (
            section.exceptPrefixes?.some(
              (e) => p === e || p.startsWith(`${e}.`),
            )
          ) {
            continue;
          }
          if (
            section.prefixes.some(
              (prefix) => p === prefix || p.startsWith(`${prefix}.`),
            )
          ) {
            dirty = true;
            break;
          }
        }
        return {
          dirty,
          restart:
            dirty &&
            changed(section.restartPick, deferredConfig, originalConfig),
        };
      });
    }
    return state;
  }, [deferredConfig, originalConfig, hasChanges, changedPaths]);

  // Check which categories have changes
  const categoryHasChanges = useMemo(() => {
    if (!hasChanges) return {};

    return {
      // Core
      [TABS.GENERAL]: !!sectionState[TABS.GENERAL]?.some((s) => s.dirty),

      // Geosite Settings
      [TABS.DOMAINS]: TAB_GROUP_PREFIXES[TABS.DOMAINS].prefixes.some((p) =>
        changedPrefix(changedPaths, p),
      ),

      // Discovery
      [TABS.DISCOVERY]: TAB_GROUP_PREFIXES[TABS.DISCOVERY].prefixes.some((p) =>
        changedPrefix(changedPaths, p),
      ),

      // MTProto
      [TABS.MTPROTO]: TAB_GROUP_PREFIXES[TABS.MTPROTO].prefixes.some((p) =>
        changedPrefix(changedPaths, p),
      ),

      // API
      [TABS.API]: TAB_GROUP_PREFIXES[TABS.API].prefixes.some((p) =>
        changedPrefix(changedPaths, p),
      ),

      // PAYLOADS
      [TABS.PAYLOADS]: false,

      // System
      [TABS.SYSTEM]: !!sectionState[TABS.SYSTEM]?.some((s) => s.dirty),
    };
  }, [changedPaths, hasChanges, sectionState]);

  const needsRestart = Object.values(sectionState).some((list) =>
    list.some((s) => s.restart),
  );

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

      if (needsRestart) {
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
    setConfig((prev) => (prev ? setByPath(prev, field, value) : prev));
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
    <ChangedFieldsProvider
      {...changeProviderProps}
    >
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
                  icon={<WarningIcon fontSize="small" />}
                  color="secondary"
                  variant="outlined"
                  sx={{
                    "& .MuiChip-icon": { marginLeft: "4px" },
                  }}
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
              {needsRestart && (
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
          {currentSections && (
            <B4Tabs
              key={currentCategory.path}
              value={currentSectionIndex}
              onChange={handleSectionChange}
              sx={{
                borderBottom: "none",
                "& .MuiTab-icon": {
                  display: { xs: "none", sm: "inline-flex" },
                },
              }}
            >
              {currentSections.map((section, index) => (
                <B4Tab
                  key={section.id}
                  icon={<section.Icon />}
                  label={t(section.labelKey)}
                  inline
                  index={index}
                  idPrefix={`${currentCategory.path}-section`}
                  hasChanges={sectionState[currentTab]?.[index]?.dirty}
                  needsRestart={sectionState[currentTab]?.[index]?.restart}
                  needsRestartLabel={t("settings.coreTabs.needsRestart")}
                />
              ))}
            </B4Tabs>
          )}
        </Box>
      </Paper>

      <Box ref={contentRef} sx={{ flex: 1, overflow: "auto", pb: 2, pr: 1, scrollbarGutter: "stable" }}>
        <TabPanel value={validTab} index={TABS.GENERAL}>
          <CoreSettings
            section={currentSectionId}
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

        <TabPanel value={validTab} index={TABS.SYSTEM}>
          <SystemSettings
            section={currentSectionId}
            config={config}
            onChange={handleChange}
          />
        </TabPanel>
      </Box>

      {/* Reset Confirmation Dialog */}
      <UnsavedChangesDialog
        open={showResetDialog}
        groups={changeGroups}
        total={changeItems.length}
        onStay={() => setShowResetDialog(false)}
        onLeave={resetChanges}
        title={t("core.discardChanges")}
        body={t("core.discardConfirm")}
        stayLabel={t("core.cancel")}
        leaveLabel={t("core.discard")}
      />

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
      <UnsavedChangesDialog
        open={blocker.state === "blocked"}
        groups={changeGroups}
        total={changeItems.length}
        onStay={() => blocker.reset?.()}
        onLeave={() => blocker.proceed?.()}
      />
    </Container>
    </ChangedFieldsProvider>
  );
}

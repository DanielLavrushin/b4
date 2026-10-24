import {
  Box,
  Button,
  CircularProgress,
  Fab,
  Paper,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams, type BlockerFunction } from "react-router";
import { useSnackbar } from "@context/SnackbarProvider";

import {
  DiscoveryIcon,
  DomainIcon,
  EscalateIcon,
  ImportExportIcon,
  RoutingIcon,
  SaveIcon,
  TcpIcon,
  UdpIcon,
} from "@b4.icons";
import ArrowBackIcon from "@mui/icons-material/ArrowBack";

import { B4Tab, B4TabPanel, B4Tabs, B4TextField } from "@b4.elements";

import { colors } from "@design";
import { rebaseDraft, setByPath } from "@utils";
import { ChangedFieldsProvider } from "@context/ChangedFieldsContext";
import { useUnsavedChangesGuard } from "@hooks/useUnsavedChangesGuard";
import {
  UnsavedChangesDialog,
  type ChangeGroupDef,
} from "@common/UnsavedChangesDialog";
import {
  useChangeItems,
  useChangeTracker,
} from "@common/useChangeDialog";
import { B4Config, B4SetConfig, SystemConfig } from "@models/config";
import { dscpEnvironment } from "@utils";
import { DiscoveryTab } from "./DiscoveryTab";
import { EscalationSettings } from "./Escalation";
import { ImportExportSettings } from "./ImportExport";
import { ShareEnvelope } from "./ShareEnvelope";
import { SetStats } from "./Manager";
import { RoutingSettings, ROUTING_SUB_TABS } from "./Routing";
import { TargetSettings, TARGET_SUB_TABS } from "./Target";
import { TcpTabContainer, TCP_SUB_TABS } from "./tcp/TcpTabContainer";
import { UdpSettings } from "./Udp";
import { FAKING_ACCORDIONS } from "./tcp/TcpFaking";
import { CallsSession } from "./udpL7";
import { useTranslation } from "react-i18next";

const EDITOR_TABS = [
  { key: "targets", labelKey: "sets.editor.tabs.targets", icon: <DomainIcon />, prefixes: ["targets"] },
  { key: "tcp", labelKey: "sets.editor.tabs.tcp", icon: <TcpIcon />, prefixes: ["tcp", "fragmentation", "faking", "mss_clamp"] },
  { key: "udp", labelKey: "sets.editor.tabs.udp", icon: <UdpIcon />, prefixes: ["udp"] },
  { key: "routing", labelKey: "sets.editor.tabs.routing", icon: <RoutingIcon />, prefixes: ["routing", "dscp", "dns"] },
  { key: "escalation", labelKey: "sets.editor.tabs.escalation", icon: <EscalateIcon />, prefixes: ["escalate"] },
  { key: "discovery", labelKey: "sets.editor.tabs.discovery", icon: <DiscoveryIcon />, prefixes: ["discovery"] },
  { key: "importExport", labelKey: "sets.editor.tabs.importExport", icon: <ImportExportIcon />, prefixes: [] as string[] },
] as const;
type EditorTabKey = (typeof EDITOR_TABS)[number]["key"];
const TAB_INDEX: Record<EditorTabKey, number> = Object.fromEntries(
  EDITOR_TABS.map((tab, index) => [tab.key, index]),
) as Record<EditorTabKey, number>;
const SET_EDITOR_DIFF_IGNORE = new Set(["id", "revision", "stats", "hub_state"]);
const SET_EDITOR_DIFF_OPTIONS = { ignoredRootKeys: SET_EDITOR_DIFF_IGNORE };
const SET_EDITOR_CHANGE_GROUPS: ChangeGroupDef[] = [
  { prefixes: ["name"], labelKey: "core.setName" },
  { prefixes: ["enabled"], labelKey: "core.enabled" },
  ...EDITOR_TABS.filter((t) => t.prefixes.length > 0)
    .map(({ prefixes, labelKey }) => ({ prefixes: [...prefixes], labelKey })),
  { prefixes: ["hub"], labelKey: "core.nav.hub" },
];

export interface SetEditorPageProps {
  settings: SystemConfig;
  set: B4SetConfig;
  config: B4Config;
  stats?: SetStats;
  otherSetsTargets?: Map<string, string[]>;
  isNew: boolean;
  saving: boolean;
  onSave: (set: B4SetConfig) => Promise<B4SetConfig | undefined> | void;
  onRefresh?: () => void;
  navigationBypassRef?: { current: boolean };
}
export const SetEditorPage = ({
  set: initialSet,
  config,
  isNew,
  settings,
  stats,
  otherSetsTargets,
  saving,
  onSave,
  onRefresh,
  navigationBypassRef,
}: SetEditorPageProps) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const requestedSub = searchParams.get("sub") ?? undefined;
  const [activeTab, setActiveTab] = useState<number>(() => {
    const index = EDITOR_TABS.findIndex((t) => t.key === requestedTab);
    return index === -1 ? 0 : index;
  });
  const [editedSet, setEditedSet] = useState<B4SetConfig | null>(initialSet);
  const [baseSet, setBaseSet] = useState(initialSet);
  const [saveInFlight, setSaveInFlight] = useState(false);
  if (!saveInFlight && baseSet !== initialSet) {
    setBaseSet(initialSet);
    setEditedSet((prev) =>
      prev ? rebaseDraft(prev, baseSet, initialSet, SET_EDITOR_DIFF_OPTIONS) : prev,
    );
  }
  const [callsSession, setCallsSession] = useState<CallsSession>({});

  useEffect(() => {
    if (!requestedTab) return;
    const index = EDITOR_TABS.findIndex((t) => t.key === requestedTab);
    if (index !== -1) setActiveTab(index);
  }, [requestedTab]);

  const handleChange = (
    field: string,
    value:
      | string
      | number
      | boolean
      | string[]
      | number[]
      | Record<string, string[]>
      | null
      | undefined,
  ) => {
    setEditedSet((prev) => (prev ? setByPath(prev, field, value) : prev));
  };

  const handleSave = () => {
    if (!editedSet) return;
    const snapshot = editedSet;
    setSaveInFlight(true);
    Promise.resolve(onSave(snapshot))
      .then((saved) => {
        setSaveInFlight(false);
        if (!saved) return;
        setBaseSet(saved);
        setEditedSet((prev) =>
          prev ? rebaseDraft(prev, snapshot, saved, SET_EDITOR_DIFF_OPTIONS) : prev,
        );
      })
      .catch(() => setSaveInFlight(false));
  };

  const handleApplyImport = (importedSet: B4SetConfig) => {
    setEditedSet(importedSet);
    setCallsSession({});
  };

  const handleBack = () => {
    navigate("/sets")?.catch(() => {});
  };
  const [showDiscardDialog, setShowDiscardDialog] = useState(false);
  const { showSuccess } = useSnackbar();
  const resetDraft = () => {
    setEditedSet(initialSet);
    setCallsSession({});
    setShowDiscardDialog(false);
    showSuccess(t("core.changesDiscarded"));
  };

  const draftForDiff = editedSet ?? initialSet;
  const dscp = useMemo(
    () => dscpEnvironment(config, initialSet.id),
    [config, initialSet.id],
  );
  const {
    changedLeaves,
    providerProps: changeProviderProps,
  } = useChangeTracker({
    draft: draftForDiff as unknown as Record<string, unknown>,
    baseline: initialSet as unknown as Record<string, unknown>,
    ignoredRootKeys: SET_EDITOR_DIFF_IGNORE,
    scope: `set:${(editedSet ?? initialSet).id}`,
  });
  const dirty = changedLeaves.length > 0;
  const changedTabs = useMemo(() => {
    const tabs = new Set<number>();
    for (const leaf of changedLeaves) {
      EDITOR_TABS.forEach((tab, index) => {
        for (const prefix of tab.prefixes) {
          if (leaf.path === prefix || leaf.path.startsWith(`${prefix}.`)) {
            tabs.add(index);
            break;
          }
        }
      });
    }
    return tabs;
  }, [changedLeaves]);
  const hasUnsavedChanges = dirty;
  const shouldBlock = useCallback<BlockerFunction>(
    ({ nextLocation }) => {
      if (navigationBypassRef?.current) {
        navigationBypassRef.current = false;
        return false;
      }
      return hasUnsavedChanges && nextLocation.pathname !== location.pathname;
    },
    [hasUnsavedChanges, location.pathname, navigationBypassRef],
  );
  const blocker = useUnsavedChangesGuard(shouldBlock, hasUnsavedChanges);
  const editedId = editedSet?.id;
  const editedName = editedSet?.name;
  const setNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const item of config.sets ?? []) {
      const nested = item as unknown as { set?: B4SetConfig };
      const set = nested.set ?? item;
      if (set.id) map.set(set.id, set.name || set.id);
    }
    if (editedId) map.set(editedId, editedName || editedId);
    return map;
  }, [config.sets, editedId, editedName]);
  const dialogsOpen = showDiscardDialog || blocker.state === "blocked";
  const tabSubDefs = useMemo(
    () => [
      ...TARGET_SUB_TABS.map((s) => ({ prefixes: s.prefixes, label: t(s.labelKey) })),
      ...TCP_SUB_TABS.map((s) => ({ prefixes: s.prefixes, label: t(s.labelKey) })),
      ...ROUTING_SUB_TABS.map((s) => ({ prefixes: s.prefixes, label: t(s.labelKey) })),
      ...FAKING_ACCORDIONS.map((s) => ({ prefixes: s.prefixes, label: t(s.labelKey) })),
    ],
    [t],
  );
  const { changeItems, changeGroups } = useChangeItems({
    changedLeaves,
    registry: changeProviderProps.registry,
    valueRegistry: changeProviderProps.valueRegistry,
    extraValueLabels: [["escalate.to", setNameById]],
    groups: SET_EDITOR_CHANGE_GROUPS,
    subDefs: tabSubDefs,
    dialogsOpen,
  });

  if (!editedSet) return null;
  let saveTooltip: string;
  if (saving) saveTooltip = t("core.saving");
  else if (isNew) saveTooltip = t("sets.editor.createSet");
  else saveTooltip = t("core.save");
  return (
    <ChangedFieldsProvider
      {...changeProviderProps}
    >
    <Box
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
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
            <Stack
              direction="row"
              spacing={2}
              alignItems="center"
              flexWrap="wrap"
              useFlexGap
              sx={{ flex: 1, minWidth: 0 }}
            >
              <Button
                startIcon={<ArrowBackIcon />}
                onClick={handleBack}
                size="small"
              >
                {t("core.back")}
              </Button>
              <B4TextField
                value={editedSet.name}
                path="name"
                onChange={(e) => {
                  handleChange("name", e.target.value);
                }}
                placeholder={t("sets.editor.namePlaceholder")}
                required
                size="small"
                sx={{
                  flex: 1,
                  minWidth: { xs: 160, sm: 250 },
                  "& .MuiInputBase-input": {
                    fontSize: "1.1rem",
                    fontWeight: 600,
                  },
                }}
              />
              {isNew && (
                <Typography
                  variant="caption"
                  sx={{
                    color: colors.secondary,
                    fontWeight: 600,
                    textTransform: "uppercase",
                    whiteSpace: "nowrap",
                  }}
                >
                  {t("sets.editor.newSet")}
                </Typography>
              )}
            </Stack>

            <Stack
              direction="row"
              spacing={1}
              justifyContent="flex-end"
              flexWrap="wrap"
              useFlexGap
              sx={{ flex: { xs: "1 1 100%", sm: "0 1 auto" } }}
            >
              <Button
                size="small"
                variant="outlined"
                onClick={() => setShowDiscardDialog(true)}
                disabled={saving || !dirty}
              >
                {t("core.discard")}
              </Button>
              <Button
                size="small"
                variant="contained"
                startIcon={
                  saving ? <CircularProgress size={16} /> : <SaveIcon />
                }
                onClick={handleSave}
                disabled={!editedSet.name.trim() || saving || (!isNew && !dirty)}
                sx={{ minWidth: 140 }}
              >
                {saving && t("core.saving")}
                {!saving && isNew && t("sets.editor.createSet")}
                {!saving && !isNew && t("core.save")}
              </Button>
            </Stack>
          </Stack>

          {/* Tabs */}
          <B4Tabs
            value={activeTab}
            onChange={(_, v: number) => {
              setActiveTab(v);
            }}
          >
            {EDITOR_TABS.map((tab, index) => (
              <B4Tab
                key={tab.key}
                icon={tab.icon}
                label={t(tab.labelKey)}
                inline
                hasChanges={changedTabs.has(index)}
                index={index}
                idPrefix="set-tab"
              />
            ))}
          </B4Tabs>
        </Box>
      </Paper>

      {/* Tab Content */}
      <Box sx={{ flex: 1, overflow: "auto", pb: 2, pr: 1, scrollbarGutter: "stable" }}>
        <B4TabPanel value={activeTab} index={TAB_INDEX.targets} idPrefix="set-tab" sx={{ pt: 3 }}>
          <TargetSettings
            initialSub={requestedSub}
            geo={settings.geo}
            config={editedSet}
            stats={stats}
            otherSetsTargets={otherSetsTargets}
            ipv4={config.queue.ipv4}
            ipv6={config.queue.ipv6}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.tcp} idPrefix="set-tab" sx={{ pt: 3 }}>
          <TcpTabContainer
            initialSub={requestedSub}
            config={editedSet}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.udp} idPrefix="set-tab" sx={{ pt: 3 }}>
          <UdpSettings
            config={editedSet}
            callsSession={callsSession}
            onCallsSessionChange={setCallsSession}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.routing} idPrefix="set-tab" sx={{ pt: 3 }}>
          <RoutingSettings
            initialSub={requestedSub}
            set={editedSet}
            ipv6={config.queue.ipv6}
            availableIfaces={config.available_ifaces ?? []}
            tunnelIfaces={config.tunnel_ifaces ?? []}
            encapsulatedIfaces={config.encapsulated_ifaces ?? []}
            dscp={dscp}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.escalation} idPrefix="set-tab" sx={{ pt: 3 }}>
          <EscalationSettings
            config={editedSet}
            allSets={config.sets ?? []}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.discovery} idPrefix="set-tab" sx={{ pt: 3 }}>
          <DiscoveryTab
            config={editedSet}
            isNew={isNew}
            dirty={dirty}
            savedWatchdog={!!initialSet.discovery?.watchdog}
            globalWatchdog={!!settings.checker?.watchdog?.enabled}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TAB_INDEX.importExport} idPrefix="set-tab" sx={{ pt: 3 }}>
          <ImportExportSettings
            config={editedSet}
            deviceFilter={dscp.deviceFilter}
            onImport={handleApplyImport}
          />
          {settings.hub?.enabled && (
            <Box sx={{ mt: 3 }}>
              <ShareEnvelope
                config={editedSet}
                isNew={isNew}
                dirty={dirty}
                onPublished={onRefresh}
              />
            </Box>
          )}
        </B4TabPanel>
      </Box>

      {(isNew || dirty) && (
        <Tooltip title={saveTooltip} placement="left">
          <span
            style={{ position: "fixed", bottom: 24, right: 24, zIndex: 1200 }}
          >
          <Fab
            size="medium"
            onClick={handleSave}
            disabled={!editedSet.name.trim() || saving || (!isNew && !dirty)}
            sx={{
              bgcolor: colors.secondary,
              color: colors.background.default,
              "&:hover": { bgcolor: colors.secondary },
              "&.Mui-disabled": {
                bgcolor: colors.border.strong,
                color: colors.background.default,
              },
            }}
          >
            {saving ? <CircularProgress size={20} /> : <SaveIcon />}
          </Fab>
        </span>
        </Tooltip>
      )}
    </Box>
      <UnsavedChangesDialog
        open={blocker.state === "blocked"}
        groups={changeGroups}
        total={changeItems.length}
        onStay={() => blocker.reset?.()}
        onLeave={() => blocker.proceed?.()}
      />
      <UnsavedChangesDialog
        open={showDiscardDialog}
        groups={changeGroups}
        total={changeItems.length}
        onStay={() => setShowDiscardDialog(false)}
        onLeave={resetDraft}
        title={t("core.discardChanges")}
        body={t("core.discardConfirm")}
        stayLabel={t("core.cancel")}
        leaveLabel={t("core.discard")}
      />
    </ChangedFieldsProvider>
  );
};

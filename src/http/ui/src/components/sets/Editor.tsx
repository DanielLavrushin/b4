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
import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

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
import { diffConfigLeaves } from "@utils";
import {
  ChangedFieldsProvider,
  useChangedFields,
} from "@context/ChangedFieldsContext";
import { useUnsavedChangesGuard } from "@hooks/useUnsavedChangesGuard";
import {
  UnsavedChangesDialog,
  groupChangeItems,
  type ChangeGroupDef,
  type UnsavedChangeItem,
} from "@common/UnsavedChangesDialog";
import { B4Config, B4SetConfig, SystemConfig } from "@models/config";
import { dscpEnvironment } from "@utils";
import { DiscoveryTab } from "./DiscoveryTab";
import { EscalationSettings } from "./Escalation";
import { ImportExportSettings } from "./ImportExport";
import { ShareEnvelope } from "./ShareEnvelope";
import { SetStats } from "./Manager";
import { RoutingSettings } from "./Routing";
import { TargetSettings } from "./Target";
import { TcpTabContainer } from "./tcp/TcpTabContainer";
import { UdpSettings } from "./Udp";
import { useTranslation } from "react-i18next";

const EDITOR_TAB_INDEX: Record<string, number> = {
  targets: 0,
  tcp: 1,
  udp: 2,
  routing: 3,
  escalation: 4,
  discovery: 5,
  importExport: 6,
};
const SET_EDITOR_DIFF_IGNORE = new Set([
  "id",
  "enabled",
  "stats",
  "hub_state",
  "hub",
  "manual_domains",
  "manual_ips",
  "geosite_domains",
  "geoip_ips",
  "total_domains",
  "total_ips",
  "asn_ips",
  "geosite_category_breakdown",
  "geoip_category_breakdown",
  "asn_breakdown",
  "asn_unresolved",
]);
const SET_EDITOR_CHANGE_GROUPS: ChangeGroupDef[] = [
  { prefixes: ["targets"], labelKey: "sets.editor.tabs.targets" },
  {
    prefixes: ["tcp", "fragmentation", "faking", "mss_clamp"],
    labelKey: "sets.editor.tabs.tcp",
  },
  { prefixes: ["udp"], labelKey: "sets.editor.tabs.udp" },
  { prefixes: ["routing", "dns"], labelKey: "sets.editor.tabs.routing" },
  { prefixes: ["escalate"], labelKey: "sets.editor.tabs.escalation" },
  { prefixes: ["discovery"], labelKey: "sets.editor.tabs.discovery" },
];
const TAB_PATH_PREFIXES: string[][] = [
  ["targets"],
  ["tcp", "fragmentation", "faking", "mss_clamp"],
  ["udp"],
  ["routing", "dns"],
  ["escalate"],
  ["discovery"],
  [],
];

export interface SetEditorPageProps {
  settings: SystemConfig;
  set: B4SetConfig;
  config: B4Config;
  stats?: SetStats;
  otherSetsTargets?: Map<string, string[]>;
  isNew: boolean;
  saving: boolean;
  onSave: (set: B4SetConfig) => void;
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
  enum TABS {
    TARGETS = 0,
    TCP,
    UDP,
    ROUTING,
    ESCALATION,
    DISCOVERY,
    IMPORT_EXPORT,
  }

  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const requestedSub = searchParams.get("sub") ?? undefined;
  const [activeTab, setActiveTab] = useState<TABS>(
    (EDITOR_TAB_INDEX[requestedTab ?? ""] ?? TABS.TARGETS),
  );
  const [editedSet, setEditedSet] = useState<B4SetConfig | null>(initialSet);

  const prevSetId = useRef(initialSet.id);
  useEffect(() => {
    setEditedSet(initialSet);
    if (prevSetId.current !== initialSet.id) {
      setActiveTab(0);
      prevSetId.current = initialSet.id;
    }
  }, [initialSet]);

  useEffect(() => {
    if (!requestedTab) return;
    const index = EDITOR_TAB_INDEX[requestedTab];
    if (index !== undefined) setActiveTab(index);
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
    setEditedSet((prev) => {
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

  const handleSave = () => {
    if (editedSet) {
      onSave(editedSet);
    }
  };

  const handleApplyImport = (importedSet: B4SetConfig) => {
    setEditedSet(importedSet);
  };

  const handleBack = () => {
    navigate("/sets")?.catch(() => {});
  };

  const draftForDiff = editedSet ?? initialSet;
  const dscp = dscpEnvironment(config, initialSet.id);
  const changedLeaves = diffConfigLeaves(
    draftForDiff as unknown as Record<string, unknown>,
    initialSet as unknown as Record<string, unknown>,
    { ignoredRootKeys: SET_EDITOR_DIFF_IGNORE },
  );
  const dirty = changedLeaves.length > 0;
  const changedPaths = new Set(changedLeaves.map((leaf) => leaf.path));
  const tabHasChanges = (tab: number) =>
    TAB_PATH_PREFIXES[tab].some((prefix) =>
      [...changedPaths].some(
        (path) => path === prefix || path.startsWith(`${prefix}.`),
      ),
    );
  const hasUnsavedChanges = dirty;
  const blocker = useUnsavedChangesGuard(
    ({ nextLocation }) => {
      if (navigationBypassRef?.current) {
        navigationBypassRef.current = false;
        return false;
      }
      return hasUnsavedChanges && nextLocation.pathname !== location.pathname;
    },
    hasUnsavedChanges,
  );
  const { snapshotLabels } = useChangedFields();
  const changeItems: UnsavedChangeItem[] = changedLeaves.map((leaf) => ({
    path: leaf.path,
    label: snapshotLabels().get(leaf.path) ?? leaf.path,
    before: leaf.before,
    after: leaf.after,
  }));
  const changeGroups = groupChangeItems(
    changeItems,
    SET_EDITOR_CHANGE_GROUPS,
  ).map((group) => ({ ...group, label: t(group.label) }));

  if (!editedSet) return null;
  let saveTooltip: string;
  if (saving) saveTooltip = t("core.saving");
  else if (isNew) saveTooltip = t("sets.editor.createSet");
  else saveTooltip = t("core.save");
  return (
    <ChangedFieldsProvider
      changedPaths={changedPaths}
      scope={`set:${editedSet.id}`}
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

            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              <Button
                size="small"
                variant="outlined"
                onClick={handleBack}
                disabled={saving}
              >
                {t("core.cancel")}
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
            <B4Tab
              icon={<DomainIcon />}
              label={t("sets.editor.tabs.targets")}
              inline
              hasChanges={tabHasChanges(TABS.TARGETS)}
              index={TABS.TARGETS}
            />
            <B4Tab
              icon={<TcpIcon />}
              label={t("sets.editor.tabs.tcp")}
              inline
              hasChanges={tabHasChanges(TABS.TCP)}
              index={TABS.TCP}
            />
            <B4Tab
              icon={<UdpIcon />}
              label={t("sets.editor.tabs.udp")}
              inline
              hasChanges={tabHasChanges(TABS.UDP)}
              index={TABS.UDP}
            />
            <B4Tab
              icon={<RoutingIcon />}
              label={t("sets.editor.tabs.routing")}
              inline
              hasChanges={tabHasChanges(TABS.ROUTING)}
              index={TABS.ROUTING}
            />
            <B4Tab
              icon={<EscalateIcon />}
              label={t("sets.editor.tabs.escalation")}
              inline
              hasChanges={tabHasChanges(TABS.ESCALATION)}
              index={TABS.ESCALATION}
            />
            <B4Tab
              icon={<DiscoveryIcon />}
              label={t("sets.editor.tabs.discovery")}
              inline
              hasChanges={tabHasChanges(TABS.DISCOVERY)}
              index={TABS.DISCOVERY}
            />
            <B4Tab
              icon={<ImportExportIcon />}
              label={t("sets.editor.tabs.importExport")}
              inline
              index={TABS.IMPORT_EXPORT}
              idPrefix="set-tab"
            />
          </B4Tabs>
        </Box>
      </Paper>

      {/* Tab Content */}
      <Box sx={{ flex: 1, overflow: "auto", pb: 2 }}>
        <B4TabPanel value={activeTab} index={TABS.TARGETS} idPrefix="set-tab" sx={{ pt: 3 }}>
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

        <B4TabPanel value={activeTab} index={TABS.TCP} idPrefix="set-tab" sx={{ pt: 3 }}>
          <TcpTabContainer
            initialSub={requestedSub}
            config={editedSet}
            queue={config.queue}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TABS.UDP} idPrefix="set-tab" sx={{ pt: 3 }}>
          <UdpSettings
            config={editedSet}
            queue={config.queue}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TABS.ROUTING} idPrefix="set-tab" sx={{ pt: 3 }}>
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

        <B4TabPanel value={activeTab} index={TABS.ESCALATION} idPrefix="set-tab" sx={{ pt: 3 }}>
          <EscalationSettings
            config={editedSet}
            allSets={config.sets ?? []}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TABS.DISCOVERY} idPrefix="set-tab" sx={{ pt: 3 }}>
          <DiscoveryTab
            config={editedSet}
            isNew={isNew}
            dirty={dirty}
            savedWatchdog={!!initialSet.discovery?.watchdog}
            globalWatchdog={!!settings.checker?.watchdog?.enabled}
            onChange={handleChange}
          />
        </B4TabPanel>

        <B4TabPanel value={activeTab} index={TABS.IMPORT_EXPORT} idPrefix="set-tab" sx={{ pt: 3 }}>
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
      <UnsavedChangesDialog
        open={blocker.state === "blocked"}
        groups={changeGroups}
        total={changeItems.length}
        onStay={() => blocker.reset?.()}
        onLeave={() => blocker.proceed?.()}
      />
    </ChangedFieldsProvider>
  );
};

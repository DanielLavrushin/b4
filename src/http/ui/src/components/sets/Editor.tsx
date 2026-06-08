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
import { useNavigate } from "react-router";

import {
  DomainIcon,
  ImportExportIcon,
  SaveIcon,
  TcpIcon,
  UdpIcon,
} from "@b4.icons";
import ArrowBackIcon from "@mui/icons-material/ArrowBack";
import AltRouteIcon from "@mui/icons-material/AltRoute";
import KeyboardDoubleArrowUpIcon from "@mui/icons-material/KeyboardDoubleArrowUp";

import { B4Tab, B4TabPanel, B4Tabs, B4TextField } from "@b4.elements";

import { colors } from "@design";
import { B4Config, B4SetConfig, SystemConfig } from "@models/config";

import { EscalationSettings } from "./Escalation";
import { ImportExportSettings } from "./ImportExport";
import { SetStats } from "./Manager";
import { RoutingSettings } from "./Routing";
import { TargetSettings } from "./Target";
import { TcpTabContainer } from "./tcp/TcpTabContainer";
import { UdpSettings } from "./Udp";
import { useTranslation } from "react-i18next";

export interface SetEditorPageProps {
  settings: SystemConfig;
  set: B4SetConfig;
  config: B4Config;
  stats?: SetStats;
  otherSetsTargets?: Map<string, string[]>;
  isNew: boolean;
  saving: boolean;
  onSave: (set: B4SetConfig) => void;
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
}: SetEditorPageProps) => {
  enum TABS {
    TARGETS = 0,
    TCP,
    UDP,
    ROUTING,
    ESCALATION,
    IMPORT_EXPORT,
  }

  const { t } = useTranslation();
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<TABS>(TABS.TARGETS);
  const [editedSet, setEditedSet] = useState<B4SetConfig | null>(initialSet);

  const prevSetId = useRef(initialSet.id);
  useEffect(() => {
    setEditedSet(initialSet);
    if (prevSetId.current !== initialSet.id) {
      setActiveTab(0);
      prevSetId.current = initialSet.id;
    }
  }, [initialSet]);

  const handleChange = (
    field: string,
    value: string | number | boolean | string[] | number[] | null | undefined,
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

  if (!editedSet) return null;

  let saveTooltip: string;
  if (saving) saveTooltip = t("core.saving");
  else if (isNew) saveTooltip = t("sets.editor.createSet");
  else saveTooltip = t("core.save");

  return (
    <>
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
            sx={{ mb: 2 }}
          >
            <Stack direction="row" spacing={2} alignItems="center">
              <Button
                startIcon={<ArrowBackIcon />}
                onClick={handleBack}
                size="small"
              >
                {t("core.back")}
              </Button>
              <B4TextField
                value={editedSet.name}
                onChange={(e) => {
                  handleChange("name", e.target.value);
                }}
                placeholder={t("sets.editor.namePlaceholder")}
                required
                size="small"
                sx={{
                  minWidth: 250,
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

            <Stack direction="row" spacing={1}>
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
                disabled={!editedSet.name.trim() || saving}
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
              index={TABS.TARGETS}
              idPrefix="set-tab"
            />
            <B4Tab
              icon={<TcpIcon />}
              label={t("sets.editor.tabs.tcp")}
              inline
              index={TABS.TCP}
              idPrefix="set-tab"
            />
            <B4Tab
              icon={<UdpIcon />}
              label={t("sets.editor.tabs.udp")}
              inline
              index={TABS.UDP}
              idPrefix="set-tab"
            />
            <B4Tab
              icon={<AltRouteIcon />}
              label={t("sets.editor.tabs.routing")}
              inline
              index={TABS.ROUTING}
              idPrefix="set-tab"
            />
            <B4Tab
              icon={<KeyboardDoubleArrowUpIcon />}
              label={t("sets.editor.tabs.escalation")}
              inline
              index={TABS.ESCALATION}
              idPrefix="set-tab"
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
            set={editedSet}
            ipv6={config.queue.ipv6}
            availableIfaces={config.available_ifaces ?? []}
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

        <B4TabPanel value={activeTab} index={TABS.IMPORT_EXPORT} idPrefix="set-tab" sx={{ pt: 3 }}>
          <ImportExportSettings
            config={editedSet}
            onImport={handleApplyImport}
          />
        </B4TabPanel>
      </Box>

      <Tooltip title={saveTooltip} placement="left">
        <span
          style={{ position: "fixed", bottom: 24, right: 24, zIndex: 1200 }}
        >
          <Fab
            size="medium"
            onClick={handleSave}
            disabled={!editedSet.name.trim() || saving}
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
    </>
  );
};

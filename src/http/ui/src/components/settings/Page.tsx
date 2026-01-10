import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import { Card } from "@design/components/ui/card";
import { Spinner } from "@design/components/ui/spinner";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import {
  ApiIcon,
  CaptureIcon,
  CoreIcon,
  DiscoveryIcon,
  GeodatIcon,
  RefreshIcon,
  SaveIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { ApiSettings } from "./Api";
import { CaptureSettings } from "./Capture";
import { ControlSettings } from "./Control";
import { DevicesSettings } from "./Devices";
import { CheckerSettings } from "./Discovery";
import { FeatureSettings } from "./Feature";
import { GeoSettings } from "./Geo";
import { LoggingSettings } from "./Logging";
import { NetworkSettings } from "./Network";

import { configApi } from "@b4.settings";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Separator } from "@design/components/ui/separator";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@design/components/ui/tabs";
import { B4Config, B4SetConfig } from "@models/config";

enum TABS {
  GENERAL = 0,
  DOMAINS,
  DISCOVERY,
  API,
  CAPTURE,
}

// Settings categories with route paths
const SETTING_CATEGORIES = [
  {
    id: TABS.GENERAL,
    path: "general",
    label: "Core",
    icon: <CoreIcon />,
    description: "Global network and queue configuration",
    requiresRestart: true,
  },
  {
    id: TABS.DOMAINS,
    path: "domains",
    label: "Geodat Settings",
    icon: <GeodatIcon />,
    description: "Global geodata configuration",
    requiresRestart: false,
  },
  {
    id: TABS.DISCOVERY,
    path: "discovery",
    label: "Discovery",
    icon: <DiscoveryIcon />,
    description: "DPI bypass domains testing",
    requiresRestart: false,
  },
  {
    id: TABS.API,
    path: "api",
    label: "API",
    icon: <ApiIcon />,
    description: "API settings for various services",
    requiresRestart: false,
  },
  {
    id: TABS.CAPTURE,
    path: "capture",
    label: "Capture",
    icon: <CaptureIcon />,
    description: "Capture real payloads from live traffic",
    requiresRestart: false,
  },
];

export function SettingsPage() {
  const { showError, showSuccess } = useSnackbar();
  const [config, setConfig] = useState<B4Config | null>(null);
  const [originalConfig, setOriginalConfig] = useState<B4Config | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showResetDialog, setShowResetDialog] = useState(false);

  const navigate = useNavigate();
  const location = useLocation();

  // Determine current tab based on URL
  const currentTabPath = location.pathname.split("/settings/")[1] || "general";
  const currentTab =
    SETTING_CATEGORIES.find((cat) => cat.path === currentTabPath)?.id ??
    TABS.GENERAL;

  // Handle tab change
  const handleTabChange = (_: React.SyntheticEvent, newValue: number) => {
    const category = SETTING_CATEGORIES.find(
      (cat) => cat.id === (newValue as TABS)
    );
    if (category) {
      navigate(`/settings/${category.path}`);
    }
  };

  // Navigate to default tab if no specific tab is in URL
  useEffect(() => {
    if (
      location.pathname === "/settings" ||
      location.pathname === "/settings/"
    ) {
      navigate("/settings/general", { replace: true });
    }
  }, [location.pathname, navigate]);

  // Check if configuration has been modified
  const hasChanges = useMemo(() => {
    if (!config || !originalConfig) return false;
    return JSON.stringify(config) !== JSON.stringify(originalConfig);
  }, [config, originalConfig]);

  // Check which categories have changes
  const categoryHasChanges = useMemo(() => {
    if (!hasChanges || !config || !originalConfig) return {};

    return {
      // Core
      [TABS.GENERAL]:
        JSON.stringify(config.system.logging) !==
          JSON.stringify(originalConfig.system.logging) ||
        JSON.stringify(config.queue) !== JSON.stringify(originalConfig.queue) ||
        JSON.stringify(config.system.web_server) !==
          JSON.stringify(originalConfig.system.web_server) ||
        JSON.stringify(config.system.tables) !==
          JSON.stringify(originalConfig.system.tables) ||
        JSON.stringify(config.queue.devices) !==
          JSON.stringify(originalConfig.queue.devices),

      // Geosite Settings
      [TABS.DOMAINS]:
        JSON.stringify(config.system.geo) !==
        JSON.stringify(originalConfig.system.geo),

      // Discovery
      [TABS.DISCOVERY]:
        JSON.stringify(config.system.checker) !==
        JSON.stringify(originalConfig.system.checker),

      // API
      [TABS.API]:
        JSON.stringify(config.system.api) !==
        JSON.stringify(originalConfig.system.api),

      // Capture
      [TABS.CAPTURE]: false,
    };
  }, [config, originalConfig, hasChanges]);

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      const data = await configApi.get();
      setConfig(data);
      setOriginalConfig(structuredClone(data));
    } catch (error) {
      console.error("Error loading configuration:", error);
      showError("Failed to load configuration");
    } finally {
      setLoading(false);
    }
  }, [showError]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const saveConfig = async () => {
    if (!config) return;

    try {
      setSaving(true);
      await configApi.save(config);
      setOriginalConfig(structuredClone(config));

      const requiresRestart = categoryHasChanges[0];
      showSuccess(
        requiresRestart
          ? "Configuration saved! Please restart B4 for core settings to take effect."
          : "Configuration saved successfully!"
      );
    } catch (error) {
      showError(error instanceof Error ? error.message : "Failed to save");
    } finally {
      setSaving(false);
      await loadConfig();
    }
  };

  const resetChanges = () => {
    if (originalConfig) {
      setConfig(structuredClone(originalConfig));
      setShowResetDialog(false);
      showSuccess("Changes discarded");
    }
  };

  const handleChange = (
    field: string,
    value:
      | string
      | number
      | boolean
      | string[]
      | B4SetConfig[]
      | null
      | undefined
  ) => {
    if (!config) return;

    const keys = field.split(".");

    if (keys.length === 1) {
      setConfig({ ...config, [field]: value });
    } else {
      const newConfig = { ...config };
      let current: Record<string, unknown> = newConfig;

      for (let i = 0; i < keys.length - 1; i++) {
        current[keys[i]] = { ...(current[keys[i]] as object) };
        current = current[keys[i]] as Record<string, unknown>;
      }

      current[keys[keys.length - 1]] = value;
      setConfig(newConfig);
    }
  };

  if (loading || !config) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
        <div className="flex flex-col items-center gap-4">
          <Spinner className="h-12 w-12" />
          <p className="text-foreground">Loading configuration...</p>
        </div>
      </div>
    );
  }

  const validTab = Math.max(currentTab, 0);

  return (
    <div className="h-full flex flex-col overflow-hidden">
      {/* Header with tabs */}
      <Card className="p-4 border border-border mb-4">
        {/* Action bar */}
        <div className="flex flex-row justify-between items-center mb-4">
          <div className="flex flex-row gap-4 items-center">
            <h6 className="text-lg font-semibold text-foreground">
              Configuration
            </h6>
            {hasChanges && (
              <Badge
                variant="secondary"
                className="inline-flex items-center gap-1"
              >
                <WarningIcon className="h-3 w-3" />
                Modified
              </Badge>
            )}
          </div>

          <div className="flex flex-row gap-2">
            {categoryHasChanges[TABS.GENERAL] && (
              <Alert variant="destructive" className="py-0 px-2">
                <AlertDescription>
                  Core settings require <strong>B4</strong> restart to take
                  effect
                </AlertDescription>
              </Alert>
            )}
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setShowResetDialog(true)}
              disabled={!hasChanges || saving}
            >
              Discard Changes
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                void loadConfig();
              }}
              disabled={saving}
            >
              <RefreshIcon className="h-4 w-4 mr-2" />
              Reload
            </Button>

            <Button
              size="sm"
              onClick={() => {
                void saveConfig();
              }}
              disabled={!hasChanges || saving}
            >
              {saving ? (
                <>
                  <Spinner className="h-4 w-4 mr-2" />
                  Saving...
                </>
              ) : (
                <>
                  <SaveIcon className="h-4 w-4 mr-2" />
                  Save Changes
                </>
              )}
            </Button>
          </div>
        </div>

        {/* Tabs */}
        <Tabs
          value={String(validTab)}
          onValueChange={(value) =>
            handleTabChange({} as React.SyntheticEvent, Number(value))
          }
          className="w-full"
        >
          <TabsList className="w-full grid grid-cols-5 mt-4">
            {SETTING_CATEGORIES.sort((a, b) => a.id - b.id).map((cat) => (
              <TabsTrigger key={cat.id} value={String(cat.id)}>
                <div className="flex items-center gap-1.5">
                  {cat.icon}
                  <span>{cat.label}</span>
                  {categoryHasChanges[cat.id] && (
                    <div className="h-1.5 w-1.5 rounded-full bg-primary" />
                  )}
                </div>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </Card>

      <div className="flex-1 overflow-auto pb-4">
        <Tabs
          value={String(validTab)}
          onValueChange={(value) =>
            handleTabChange({} as React.SyntheticEvent, Number(value))
          }
          className="w-full"
        >
          <TabsContent value={String(TABS.GENERAL)} className="mt-2">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6 items-stretch">
              <div className="col-span-1 md:col-span-2">
                <NetworkSettings config={config} onChange={handleChange} />
              </div>

              <div className="col-span-1 flex">
                <div className="w-full">
                  <ControlSettings loadConfig={() => void loadConfig()} />
                </div>
              </div>
              <div className="col-span-1 flex">
                <div className="w-full">
                  <LoggingSettings config={config} onChange={handleChange} />
                </div>
              </div>

              <div className="col-span-1">
                <FeatureSettings config={config} onChange={handleChange} />
              </div>

              <div className="col-span-1">
                <DevicesSettings config={config} onChange={handleChange} />
              </div>
            </div>
          </TabsContent>

          <TabsContent value={String(TABS.DOMAINS)} className="mt-2">
            <GeoSettings
              config={config}
              onChange={handleChange}
              loadConfig={() => {
                void loadConfig();
              }}
            />
          </TabsContent>

          <TabsContent value={String(TABS.API)} className="mt-2">
            <ApiSettings config={config} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.DISCOVERY)} className="mt-2">
            <CheckerSettings config={config} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.CAPTURE)} className="mt-2">
            <CaptureSettings />
          </TabsContent>
        </Tabs>
      </div>

      {/* Reset Confirmation Dialog */}
      <Dialog
        open={showResetDialog}
        onOpenChange={(open) => !open && setShowResetDialog(false)}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Discard changes</DialogTitle>
            <DialogDescription>
              Are you sure you want to discard all unsaved changes? This action
              cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <Separator />
          <DialogFooter>
            <Button onClick={() => setShowResetDialog(false)}>Cancel</Button>
            <div className="flex-1" />
            <Button onClick={resetChanges}>
              Discard Changes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

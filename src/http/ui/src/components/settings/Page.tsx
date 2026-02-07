import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Card, CardContent, CardHeader, CardTitle } from "@primitives/card";
import { Spinner } from "@primitives/spinner";
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
import { B4Config, B4SetConfig } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import { Separator } from "@primitives/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@primitives/tabs";

enum TABS {
  CORE = 0,
  GEODAT,
  DISCOVERY,
  API,
  CAPTURE,
}

// hasChanges utilities
const deepEqual = (a: unknown, b: unknown): boolean => {
  return JSON.stringify(a) === JSON.stringify(b);
};

const getNestedValue = (obj: unknown, path: string): unknown => {
  if (typeof obj !== "object" || obj === null) return undefined;
  let current: unknown = obj;
  for (const key of path.split(".")) {
    if (current && typeof current === "object" && key in current) {
      current = (current as Record<string, unknown>)[key];
    } else {
      return undefined;
    }
  }
  return current;
};

const CATEGORY_CONFIG_PATHS: Record<TABS, string[]> = {
  [TABS.CORE]: [
    "system.logging",
    "queue",
    "system.web_server",
    "system.tables",
    "queue.devices",
  ],
  [TABS.GEODAT]: ["system.geo"],
  [TABS.DISCOVERY]: ["system.checker"],
  [TABS.API]: ["system.api"],
  [TABS.CAPTURE]: [],
};

//

const SETTING_CATEGORIES = [
  {
    id: TABS.CORE,
    path: "core",
    label: "Core",
    icon: <CoreIcon />,
    description: "Global network and queue configuration",
    requiresRestart: true,
  },
  {
    id: TABS.GEODAT,
    path: "geodat",
    label: "Geodat",
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
  const currentTab = useMemo(() => {
    const path = location.pathname.split("/settings/")[1] || "core";
    return SETTING_CATEGORIES.find((cat) => cat.path === path)?.id ?? TABS.CORE;
  }, [location.pathname]);

  // Navigate to default tab if no specific tab is in URL
  useEffect(() => {
    if (
      location.pathname === "/settings" ||
      location.pathname === "/settings/"
    ) {
      navigate("/settings/core", { replace: true });
    }
  }, [location.pathname, navigate]);

  // Handle tab change
  const handleTabChange = useCallback(
    (value: string) => {
      const tabId = Number(value) as TABS;
      const category = SETTING_CATEGORIES.find((cat) => cat.id === tabId);
      if (category) {
        navigate(`/settings/${category.path}`);
      }
    },
    [navigate],
  );

  // Check if configuration has been modified
  const hasChanges = useMemo(() => {
    if (!config || !originalConfig) return false;
    return !deepEqual(config, originalConfig);
  }, [config, originalConfig]);

  // Check which categories have changes
  const categoryHasChanges = useMemo(() => {
    if (!hasChanges || !config || !originalConfig)
      return {} as Record<TABS, boolean>;

    const changes: Record<TABS, boolean> = {} as Record<TABS, boolean>;

    (Object.keys(CATEGORY_CONFIG_PATHS) as unknown as TABS[]).forEach((tab) => {
      const paths = CATEGORY_CONFIG_PATHS[tab];
      changes[tab] = paths.some((path) => {
        const current = getNestedValue(config, path);
        const original = getNestedValue(originalConfig, path);
        return !deepEqual(current, original);
      });
    });

    return changes;
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

      const requiresRestart = categoryHasChanges[TABS.CORE];
      showSuccess(
        requiresRestart
          ? "Configuration saved! Please restart B4 for core settings to take effect."
          : "Configuration saved successfully!",
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
      | undefined,
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
      <div className="bg-background/80 fixed inset-0 z-50 flex items-center justify-center backdrop-blur-sm">
        <div className="flex flex-col items-center gap-4">
          <Spinner className="size-12" />
          <p className="text-foreground">Loading configuration...</p>
        </div>
      </div>
    );
  }

  const tabValue = String(Math.max(currentTab, 0));

  return (
    <div className="flex h-full flex-col gap-6 overflow-hidden">
      <Card>
        <CardHeader>
          <div className="mb-4 flex flex-row items-center justify-between">
            <div className="flex flex-row items-center gap-4">
              <CardTitle className="text-lg font-semibold">
                Configuration
              </CardTitle>
              {hasChanges && (
                <Badge variant="secondary">
                  <WarningIcon />
                  Modified
                </Badge>
              )}
            </div>

            <div className="flex flex-row gap-2">
              {categoryHasChanges[TABS.CORE] && (
                <Alert variant="destructive">
                  <AlertDescription>
                    Core settings require <strong>B4</strong> restart
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
                onClick={() => void loadConfig()}
                disabled={saving}
              >
                <RefreshIcon />
                Reload
              </Button>
              <Button
                size="sm"
                onClick={() => void saveConfig()}
                disabled={!hasChanges || saving}
              >
                {saving ? (
                  <>
                    <Spinner />
                    Saving...
                  </>
                ) : (
                  <>
                    <SaveIcon />
                    Save Changes
                  </>
                )}
              </Button>
            </div>
          </div>
        </CardHeader>

        <CardContent>
          <Tabs value={tabValue} onValueChange={handleTabChange}>
            <TabsList className="w-full">
              {SETTING_CATEGORIES.map((cat) => (
                <TabsTrigger key={cat.id} value={String(cat.id)}>
                  <div className="flex items-center gap-1.5">
                    {cat.icon}
                    <span className="hidden lg:inline">{cat.label}</span>
                    {categoryHasChanges[cat.id] && (
                      <div className="bg-primary h-1.5 w-1.5" />
                    )}
                  </div>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </CardContent>
      </Card>

      <div className="flex-1 overflow-auto">
        <Tabs
          value={tabValue}
          onValueChange={handleTabChange}
          className="w-full"
        >
          <TabsContent value={String(TABS.CORE)}>
            <div className="flex flex-col gap-6">
              <div className="grid grid-cols-1 items-stretch gap-6 md:grid-cols-2">
                <NetworkSettings config={config} onChange={handleChange} />
                <FeatureSettings config={config} onChange={handleChange} />
                <LoggingSettings config={config} onChange={handleChange} />
                <ControlSettings loadConfig={() => void loadConfig()} />
              </div>
              <DevicesSettings config={config} onChange={handleChange} />
            </div>
          </TabsContent>

          <TabsContent value={String(TABS.GEODAT)}>
            <GeoSettings
              config={config}
              onChange={handleChange}
              loadConfig={() => void loadConfig()}
            />
          </TabsContent>

          <TabsContent value={String(TABS.API)}>
            <ApiSettings config={config} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.DISCOVERY)}>
            <CheckerSettings config={config} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.CAPTURE)}>
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
            <Button onClick={resetChanges}>Discard Changes</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

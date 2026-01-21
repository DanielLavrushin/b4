import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { v4 as uuidv4 } from "uuid";

import {
  DnsIcon,
  DomainIcon,
  FakingIcon,
  FragIcon,
  ImportExportIcon,
  RefreshIcon,
  SaveIcon,
  TcpIcon,
  UdpIcon,
  WarningIcon,
  IconGoBack,
} from "@b4.icons";

import { useSnackbar } from "@context/SnackbarProvider";
import { useSets } from "@hooks/useSets";
import {
  B4Config,
  B4SetConfig,
  MAIN_SET_ID,
  SystemConfig,
} from "@models/config";

import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Card, CardContent, CardHeader, CardTitle } from "@primitives/card";
import { Field, FieldDescription, FieldLabel } from "@primitives/field";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";
import { Spinner } from "@primitives/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@primitives/tabs";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";

import { DnsSettings } from "./Dns";
import { FakingSettings } from "./Faking";
import { FragmentationSettings } from "./Fragmentation";
import { ImportExportSettings } from "./ImportExport";
import { SetStats, SetWithStats } from "./Manager";
import { TargetSettings } from "./Target";
import { TcpSettings } from "./Tcp";
import { UdpSettings } from "./Udp";

enum TABS {
  TARGETS = 0,
  TCP,
  UDP,
  DNS,
  FRAGMENTATION,
  FAKING,
  IMPORT_EXPORT,
}

const TAB_CATEGORIES = [
  {
    id: TABS.TARGETS,
    path: "targets",
    label: "Targets",
    icon: <DomainIcon />,
  },
  {
    id: TABS.TCP,
    path: "tcp",
    label: "TCP",
    icon: <TcpIcon />,
  },
  {
    id: TABS.UDP,
    path: "udp",
    label: "UDP",
    icon: <UdpIcon />,
  },
  {
    id: TABS.DNS,
    path: "dns",
    label: "DNS",
    icon: <DnsIcon />,
  },
  {
    id: TABS.FRAGMENTATION,
    path: "fragmentation",
    label: "Fragmentation",
    icon: <FragIcon />,
  },
  {
    id: TABS.FAKING,
    path: "faking",
    label: "Faking",
    icon: <FakingIcon />,
  },
  {
    id: TABS.IMPORT_EXPORT,
    path: "import-export",
    label: "Import/Export",
    icon: <ImportExportIcon />,
  },
];

function createDefaultSet(): B4SetConfig {
  return {
    id: uuidv4(),
    name: "",
    enabled: true,
    tcp: {
      conn_bytes_limit: 19,
      seg2delay: 0,
      syn_fake: false,
      syn_fake_len: 0,
      syn_ttl: 3,
      drop_sack: false,
      win: { mode: "off", values: [0, 1460, 8192, 65535] },
      desync: { mode: "off", ttl: 3, count: 3, post_desync: false },
      incoming: {
        mode: "off",
        min: 14,
        max: 14,
        fake_ttl: 3,
        fake_count: 3,
        strategy: "badsum",
      },
    } as B4SetConfig["tcp"],
    udp: {
      mode: "fake",
      fake_seq_length: 6,
      fake_len: 64,
      faking_strategy: "none",
      dport_filter: "",
      filter_quic: "disabled",
      filter_stun: true,
      conn_bytes_limit: 8,
      seg2delay: 0,
    } as B4SetConfig["udp"],
    dns: {
      enabled: false,
      target_dns: "",
      fragment_query: false,
    } as B4SetConfig["dns"],
    fragmentation: {
      strategy: "tcp",
      reverse_order: true,
      middle_sni: true,
      sni_position: 1,
      oob_position: 0,
      oob_char: 120,
      tlsrec_pos: 0,
      seq_overlap: 0,
      seq_overlap_pattern: [],
      combo: {
        extension_split: true,
        first_byte_split: true,
        shuffle_mode: "middle",
        first_delay_ms: 100,
        jitter_max_us: 2000,
        decoy_enabled: false,
        decoy_snis: ["ya.ru", "vk.com", "mail.ru"],
      },
      disorder: {
        shuffle_mode: "full",
        min_jitter_us: 1000,
        max_jitter_us: 3000,
      },
    } as B4SetConfig["fragmentation"],
    faking: {
      sni: true,
      ttl: 8,
      strategy: "pastseq",
      seq_offset: 10000,
      sni_seq_length: 1,
      sni_type: 2,
      custom_payload: "",
      payload_file: "",
      tls_mod: [] as string[],
      sni_mutation: {
        mode: "off",
        grease_count: 3,
        padding_size: 2048,
        fake_ext_count: 5,
        fake_snis: ["ya.ru", "vk.com", "max.ru"],
      },
    } as B4SetConfig["faking"],
    targets: {
      sni_domains: [],
      ip: [],
      geosite_categories: [],
      geoip_categories: [],
    } as B4SetConfig["targets"],
  };
}

interface SetEditorPageProps {
  config: B4Config & { sets?: SetWithStats[] };
  onRefresh: () => void;
}

export function SetEditorPage({ config, onRefresh }: SetEditorPageProps) {
  const { showError, showSuccess } = useSnackbar();
  const navigate = useNavigate();
  const location = useLocation();
  const { setId } = useParams<{ setId: string }>();

  const { createSet, updateSet, loading: saving } = useSets();

  const isNew = setId === "new";

  // Find the set to edit
  const setsData = config.sets || [];
  const sets = setsData.map((s) => ("set" in s ? s.set : s)) as B4SetConfig[];
  const setsStats = setsData.map((s) =>
    "stats" in s ? s.stats : null,
  ) as (SetStats | null)[];

  const originalSet = isNew ? null : sets.find((s) => s.id === setId) || null;

  const stats = isNew
    ? undefined
    : setsStats[sets.findIndex((s) => s.id === setId)] || undefined;

  const [editedSet, setEditedSet] = useState<B4SetConfig | null>(null);
  const [originalEditedSet, setOriginalEditedSet] =
    useState<B4SetConfig | null>(null);
  const [showDiscardDialog, setShowDiscardDialog] = useState(false);

  // Initialize edited set
  useEffect(() => {
    if (isNew) {
      const newSet = createDefaultSet();
      newSet.name = `Set ${sets.length + 1}`;
      setEditedSet(newSet);
      setOriginalEditedSet(structuredClone(newSet));
    } else if (originalSet) {
      setEditedSet(structuredClone(originalSet));
      setOriginalEditedSet(structuredClone(originalSet));
    }
  }, [isNew, originalSet, sets.length]);

  // Determine current tab from URL
  const pathParts = location.pathname.split("/");
  const tabPath = pathParts[pathParts.length - 1];
  const currentTab =
    TAB_CATEGORIES.find((cat) => cat.path === tabPath)?.id ?? TABS.TARGETS;

  // Redirect to default tab if needed
  useEffect(() => {
    const validTabPaths = TAB_CATEGORIES.map((c) => c.path);
    if (!validTabPaths.includes(tabPath)) {
      navigate(`/sets/${setId}/targets`, { replace: true });
    }
  }, [tabPath, setId, navigate]);

  const handleTabChange = useCallback(
    (newValue: number) => {
      const category = TAB_CATEGORIES.find(
        (cat) => cat.id === (newValue as TABS),
      );
      if (category) {
        navigate(`/sets/${setId}/${category.path}`);
      }
    },
    [setId, navigate],
  );

  const hasChanges = useMemo(() => {
    if (!editedSet || !originalEditedSet) return false;
    return JSON.stringify(editedSet) !== JSON.stringify(originalEditedSet);
  }, [editedSet, originalEditedSet]);

  const handleChange = useCallback(
    (
      field: string,
      value: string | number | boolean | string[] | number[] | null | undefined,
    ) => {
      setEditedSet((prev) => {
        if (!prev) return prev;

        const keys = field.split(".");

        if (keys.length === 1) {
          return { ...prev, [field]: value };
        } else {
          const newConfig = { ...prev };
          let current: Record<string, unknown> = newConfig;

          for (let i = 0; i < keys.length - 1; i++) {
            current[keys[i]] = { ...(current[keys[i]] as object) };
            current = current[keys[i]] as Record<string, unknown>;
          }

          current[keys[keys.length - 1]] = value;
          return newConfig;
        }
      });
    },
    [],
  );

  const handleSave = useCallback(async () => {
    if (!editedSet) return;

    if (!editedSet.name.trim()) {
      showError("Set name is required");
      return;
    }

    const result = isNew
      ? await createSet(editedSet)
      : await updateSet(editedSet);

    if (result.success) {
      showSuccess(isNew ? "Set created" : "Set updated");
      onRefresh();
      navigate("/sets");
    } else {
      showError(result.error || "Failed to save");
    }
  }, [
    editedSet,
    isNew,
    createSet,
    updateSet,
    showSuccess,
    showError,
    onRefresh,
    navigate,
  ]);

  const handleCancel = useCallback(() => {
    if (hasChanges) {
      setShowDiscardDialog(true);
    } else {
      navigate("/sets");
    }
  }, [hasChanges, navigate]);

  const handleDiscard = useCallback(() => {
    setShowDiscardDialog(false);
    navigate("/sets");
  }, [navigate]);

  const handleReload = useCallback(() => {
    if (originalEditedSet) {
      setEditedSet(structuredClone(originalEditedSet));
    }
  }, [originalEditedSet]);

  const handleApplyImport = useCallback(
    (importedSet: B4SetConfig) => {
      setEditedSet(importedSet);
      navigate(`/sets/${setId}/targets`);
    },
    [setId, navigate],
  );

  // If set not found (and not new)
  if (!isNew && !originalSet) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4">
        <p className="text-muted-foreground">Set not found</p>
        <Button onClick={() => navigate("/sets")}>
          <IconGoBack className="mr-2 size-4" />
          Back to Sets
        </Button>
      </div>
    );
  }

  if (!editedSet) {
    return (
      <div className="bg-background/80 fixed inset-0 z-50 flex items-center justify-center backdrop-blur-sm">
        <div className="flex flex-col items-center gap-4">
          <Spinner className="size-12" />
          <p className="text-foreground">Loading...</p>
        </div>
      </div>
    );
  }

  const mainSetConfig: B4SetConfig =
    sets.find((s) => s.id === MAIN_SET_ID) || editedSet;

  const settings: SystemConfig = config.system;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header with tabs */}
      <Card>
        {/* Action bar */}
        <CardHeader className="flex flex-row items-center justify-between">
          <div className="flex flex-row items-center gap-4">
            <Button variant="ghost" size="sm" onClick={handleCancel}>
              <IconGoBack />
              Back
            </Button>
            <Separator orientation="vertical" />
            <CardTitle className="text-lg font-semibold">
              {isNew ? "Create New Set" : `Edit Set: ${editedSet.name}`}
            </CardTitle>
            {hasChanges && (
              <Badge variant="secondary">
                <WarningIcon />
                Modified
              </Badge>
            )}
          </div>

          <div className="flex flex-row gap-2">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setShowDiscardDialog(true)}
              disabled={!hasChanges || saving}
            >
              Discard Changes
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleReload}
              disabled={saving}
            >
              <RefreshIcon />
              Reload
            </Button>
            <Button
              size="sm"
              onClick={() => void handleSave()}
              disabled={!editedSet.name.trim() || saving}
            >
              {saving ? (
                <>
                  <Spinner />
                  Saving...
                </>
              ) : (
                <>
                  <SaveIcon />
                  {isNew ? "Create Set" : "Save Changes"}
                </>
              )}
            </Button>
          </div>
        </CardHeader>

        {/* Set Name */}
        <CardContent>
          <Input
            value={editedSet.name}
            onChange={(e) => handleChange("name", e.target.value)}
            placeholder="e.g., YouTube Bypass, Gaming, Streaming"
            required
            className="my-4"
          />

          {/* Tabs */}

          <Tabs
            value={String(currentTab)}
            onValueChange={(value) => handleTabChange(Number(value))}
          >
            <TabsList className="w-full">
              {TAB_CATEGORIES.map((cat) => (
                <TabsTrigger key={cat.id} value={String(cat.id)}>
                  <div className="flex items-center gap-1.5">
                    {cat.icon}
                    <span className="hidden lg:inline">{cat.label}</span>
                  </div>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </CardContent>
      </Card>

      {/* Tab content */}
      <div className="flex-1 overflow-auto pb-4">
        <Tabs
          value={String(currentTab)}
          onValueChange={(value) => handleTabChange(Number(value))}
          className="w-full"
        >
          <TabsContent value={String(TABS.TARGETS)} className="mt-2">
            <TargetSettings
              geo={settings.geo}
              config={editedSet}
              stats={stats}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={String(TABS.TCP)} className="mt-2">
            <TcpSettings
              config={editedSet}
              main={mainSetConfig}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={String(TABS.UDP)} className="mt-2">
            <UdpSettings
              config={editedSet}
              main={mainSetConfig}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={String(TABS.DNS)} className="mt-2">
            <DnsSettings
              config={editedSet}
              onChange={handleChange}
              ipv6={config.queue.ipv6}
            />
          </TabsContent>

          <TabsContent value={String(TABS.FRAGMENTATION)} className="mt-2">
            <FragmentationSettings config={editedSet} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.FAKING)} className="mt-2">
            <FakingSettings config={editedSet} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={String(TABS.IMPORT_EXPORT)} className="mt-2">
            <ImportExportSettings
              config={editedSet}
              onImport={handleApplyImport}
            />
          </TabsContent>
        </Tabs>
      </div>

      {/* Discard Confirmation Dialog */}
      <Dialog
        open={showDiscardDialog}
        onOpenChange={(open) => !open && setShowDiscardDialog(false)}
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
            <Button onClick={() => setShowDiscardDialog(false)}>Cancel</Button>
            <div className="flex-1" />
            <Button onClick={handleDiscard}>Discard Changes</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

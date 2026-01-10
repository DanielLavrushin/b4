import { useEffect, useState } from "react";

import {
  ImportExportIcon,
  DnsIcon,
  FakingIcon,
  FragIcon,
  TcpIcon,
  UdpIcon,
  DomainIcon,
} from "@b4.icons";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@design/components/ui/tabs";

import { Button } from "@design/components/ui/button";
import { Spinner } from "@design/components/ui/spinner";
import {
  B4Config,
  B4SetConfig,
  MAIN_SET_ID,
  SystemConfig,
} from "@models/config";

import { DnsSettings } from "./Dns";
import { FakingSettings } from "./Faking";
import { FragmentationSettings } from "./Fragmentation";
import { ImportExportSettings } from "./ImportExport";
import { SetStats } from "./Manager";
import { TargetSettings } from "./Target";
import { TcpSettings } from "./Tcp";
import { UdpSettings } from "./Udp";

export interface SetEditorProps {
  open: boolean;
  settings: SystemConfig;
  set: B4SetConfig;
  config: B4Config;
  stats?: SetStats;
  isNew: boolean;
  saving: boolean;
  onClose: () => void;
  onSave: (set: B4SetConfig) => void;
}

export const SetEditor = ({
  open,
  set: initialSet,
  config,
  isNew,
  settings,
  stats,
  saving,
  onClose,
  onSave,
}: SetEditorProps) => {
  enum TABS {
    TARGETS = 0,
    TCP,
    UDP,
    DNS,
    FRAGMENTATION,
    FAKING,
    IMPORT_EXPORT,
  }

  const [activeTab, setActiveTab] = useState<TABS>(TABS.TARGETS);
  const [editedSet, setEditedSet] = useState<B4SetConfig | null>(initialSet);

  const mainSet = config.sets.find((s) => s.id === MAIN_SET_ID)!;

  useEffect(() => {
    setEditedSet(initialSet);
    setActiveTab(0);
  }, [initialSet]);

  const handleChange = (
    field: string,
    value: string | number | boolean | string[] | number[] | null | undefined
  ) => {
    if (!editedSet) return;

    const keys = field.split(".");

    if (keys.length === 1) {
      setEditedSet({ ...editedSet, [field]: value });
    } else {
      const newConfig = { ...editedSet };
      let current: Record<string, unknown> = newConfig;

      for (let i = 0; i < keys.length - 1; i++) {
        current[keys[i]] = { ...(current[keys[i]] as object) };
        current = current[keys[i]] as Record<string, unknown>;
      }

      current[keys[keys.length - 1]] = value;
      setEditedSet(newConfig);
    }
  };

  const handleSave = () => {
    if (editedSet) {
      onSave(editedSet);
    }
  };

  const handleApplyImport = (importedSet: B4SetConfig) => {
    setEditedSet(importedSet);
    setActiveTab(TABS.TARGETS);
  };

  if (!editedSet) return null;

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="max-h-[90vh] flex flex-col max-w-[90vw] sm:max-w-[90vw] w-[90vw]">
        <DialogHeader>
          <DialogTitle>
            {isNew ? "Create New Set" : `Edit Set: ${editedSet.name}`}
          </DialogTitle>
          <DialogDescription>
            {isNew
              ? "Configure your new set settings"
              : "Modify set configuration and settings"}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-hidden flex flex-col gap-4">
          <Field>
            <FieldLabel>Set Name</FieldLabel>
            <Input
              value={editedSet.name}
              onChange={(e) => handleChange("name", e.target.value)}
              placeholder="e.g., YouTube Bypass, Gaming, Streaming"
              required
            />
            <FieldDescription>
              Give this set a descriptive name
            </FieldDescription>
          </Field>

          <Tabs
            value={activeTab.toString()}
            onValueChange={(v) => setActiveTab(Number(v) as TABS)}
            className="flex-1 flex flex-col overflow-hidden"
          >
            <TabsList className="grid w-full grid-cols-7">
              <TabsTrigger value={TABS.TARGETS.toString()}>
                <DomainIcon className="h-4 w-4 mr-2" />
                Targets
              </TabsTrigger>
              <TabsTrigger value={TABS.TCP.toString()}>
                <TcpIcon className="h-4 w-4 mr-2" />
                TCP
              </TabsTrigger>
              <TabsTrigger value={TABS.UDP.toString()}>
                <UdpIcon className="h-4 w-4 mr-2" />
                UDP
              </TabsTrigger>
              <TabsTrigger value={TABS.DNS.toString()}>
                <DnsIcon className="h-4 w-4 mr-2" />
                DNS
              </TabsTrigger>
              <TabsTrigger value={TABS.FRAGMENTATION.toString()}>
                <FragIcon className="h-4 w-4 mr-2" />
                Fragmentation
              </TabsTrigger>
              <TabsTrigger value={TABS.FAKING.toString()}>
                <FakingIcon className="h-4 w-4 mr-2" />
                Faking
              </TabsTrigger>
              <TabsTrigger value={TABS.IMPORT_EXPORT.toString()}>
                <ImportExportIcon className="h-4 w-4 mr-2" />
                Import/Export
              </TabsTrigger>
            </TabsList>

            <div className="flex-1 overflow-y-auto mt-4">
              <TabsContent value={TABS.TARGETS.toString()} className="mt-0">
                <div className="flex flex-col gap-4">
                  <TargetSettings
                    geo={settings.geo}
                    config={editedSet}
                    stats={stats}
                    onChange={handleChange}
                  />
                </div>
              </TabsContent>

              <TabsContent value={TABS.TCP.toString()} className="mt-0">
                <div className="flex flex-col gap-4">
                  <TcpSettings
                    config={editedSet}
                    main={mainSet}
                    onChange={handleChange}
                  />
                </div>
              </TabsContent>

              <TabsContent value={TABS.UDP.toString()} className="mt-0">
                <div className="flex flex-col gap-4">
                  <UdpSettings
                    config={editedSet}
                    main={mainSet}
                    onChange={handleChange}
                  />
                </div>
              </TabsContent>

              <TabsContent value={TABS.DNS.toString()} className="mt-0">
                <div className="flex flex-col gap-4">
                  <DnsSettings
                    config={editedSet}
                    onChange={handleChange}
                    ipv6={config.queue.ipv6}
                  />
                </div>
              </TabsContent>

              <TabsContent
                value={TABS.FRAGMENTATION.toString()}
                className="mt-0"
              >
                <div className="flex flex-col gap-4">
                  <FragmentationSettings
                    config={editedSet}
                    onChange={handleChange}
                  />
                </div>
              </TabsContent>

              <TabsContent value={TABS.FAKING.toString()} className="mt-0">
                <div className="flex flex-col gap-4">
                  <FakingSettings config={editedSet} onChange={handleChange} />
                </div>
              </TabsContent>

              <TabsContent
                value={TABS.IMPORT_EXPORT.toString()}
                className="mt-0"
              >
                <div className="flex flex-col gap-4">
                  <ImportExportSettings
                    config={editedSet}
                    onImport={handleApplyImport}
                  />
                </div>
              </TabsContent>
            </div>
          </Tabs>
        </div>

        <DialogFooter>
          <Button onClick={onClose} variant="outline" disabled={saving}>
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={!editedSet.name.trim() || saving}
            className="min-w-35"
          >
            {saving ? (
              <>
                <Spinner className="h-4 w-4 mr-2" />
                Saving...
              </>
            ) : isNew ? (
              "Create Set"
            ) : (
              "Save Changes"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

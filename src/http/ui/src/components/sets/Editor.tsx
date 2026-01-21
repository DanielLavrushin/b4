import { useEffect, useState } from "react";

import {
  DnsIcon,
  DomainIcon,
  FakingIcon,
  FragIcon,
  ImportExportIcon,
  TcpIcon,
  UdpIcon,
} from "@b4.icons";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import { Field, FieldDescription, FieldLabel } from "@primitives/field";
import { Input } from "@primitives/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@primitives/tabs";

import {
  B4Config,
  B4SetConfig,
  MAIN_SET_ID,
  SystemConfig,
} from "@models/config";
import { Button } from "@primitives/button";
import { Spinner } from "@primitives/spinner";

import { DnsSettings } from "./Dns";
import { FakingSettings } from "./Faking";
import { FragmentationSettings } from "./Fragmentation";
import { ImportExportSettings } from "./ImportExport";
import { SetStats } from "./Manager";
import { TargetSettings } from "./Target";
import { TcpSettings } from "./Tcp";
import { UdpSettings } from "./Udp";
import { Separator } from "@design/primitives/separator";

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
      <DialogContent>
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

        <Separator />

        <Field>
          <FieldLabel>Set Name</FieldLabel>
          <Input
            value={editedSet.name}
            onChange={(e) => handleChange("name", e.target.value)}
            placeholder="e.g., YouTube Bypass, Gaming, Streaming"
            required
          />
          <FieldDescription>Give this set a descriptive name</FieldDescription>
        </Field>

        <Tabs
          value={activeTab.toString()}
          onValueChange={(v) => setActiveTab(Number(v) as TABS)}
          className="overflow-hidden"
        >
          <TabsList className="flex w-full flex-wrap md:flex-nowrap">
            <TabsTrigger value={TABS.TARGETS.toString()}>
              <DomainIcon />
              Targets
            </TabsTrigger>
            <TabsTrigger value={TABS.TCP.toString()}>
              <TcpIcon />
              TCP
            </TabsTrigger>
            <TabsTrigger value={TABS.UDP.toString()}>
              <UdpIcon />
              UDP
            </TabsTrigger>
            <TabsTrigger value={TABS.DNS.toString()}>
              <DnsIcon />
              DNS
            </TabsTrigger>
            <TabsTrigger value={TABS.FRAGMENTATION.toString()}>
              <FragIcon />
              Fragmentation
            </TabsTrigger>
            <TabsTrigger value={TABS.FAKING.toString()}>
              <FakingIcon />
              Faking
            </TabsTrigger>
            <TabsTrigger value={TABS.IMPORT_EXPORT.toString()}>
              <ImportExportIcon />
              Import/Export
            </TabsTrigger>
          </TabsList>

          <TabsContent value={TABS.TARGETS.toString()}>
            <TargetSettings
              geo={settings.geo}
              config={editedSet}
              stats={stats}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={TABS.TCP.toString()}>
            <TcpSettings
              config={editedSet}
              main={mainSet}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={TABS.UDP.toString()}>
            <UdpSettings
              config={editedSet}
              main={mainSet}
              onChange={handleChange}
            />
          </TabsContent>

          <TabsContent value={TABS.DNS.toString()}>
            <DnsSettings
              config={editedSet}
              onChange={handleChange}
              ipv6={config.queue.ipv6}
            />
          </TabsContent>

          <TabsContent value={TABS.FRAGMENTATION.toString()}>
            <FragmentationSettings config={editedSet} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={TABS.FAKING.toString()}>
            <FakingSettings config={editedSet} onChange={handleChange} />
          </TabsContent>

          <TabsContent value={TABS.IMPORT_EXPORT.toString()}>
            <ImportExportSettings
              config={editedSet}
              onImport={handleApplyImport}
            />
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button onClick={onClose} variant="outline" disabled={saving}>
            Cancel
          </Button>
          <Button
            onClick={handleSave}
            disabled={!editedSet.name.trim() || saving}
          >
            {saving ? (
              <>
                <Spinner />
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

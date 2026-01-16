import { DeviceInfo, DevicesSettingsProps, useDevices } from "@b4.devices";
import { B4InlineEdit } from "@b4.elements";
import {
  DeviceUnknowIcon,
  EditIcon,
  RefreshIcon,
  RestoreIcon,
} from "@b4.icons";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { Checkbox } from "@primitives/checkbox";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldTitle,
} from "@primitives/field";
import { Spinner } from "@primitives/spinner";
import { Switch } from "@primitives/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@primitives/tooltip";
import { cn } from "@design/lib/utils";
import { useEffect, useState } from "react";

const DeviceNameCell = ({
  device,
  isSelected,
  isEditing,
  onStartEdit,
  onSaveAlias,
  onResetAlias,
  onCancelEdit,
}: {
  device: DeviceInfo;
  isSelected: boolean;
  isEditing: boolean;
  onStartEdit: () => void;
  onSaveAlias: (alias: string) => Promise<void>;
  onResetAlias: () => Promise<void>;
  onCancelEdit: () => void;
}) => {
  const displayName = device.alias || device.vendor;

  if (isEditing) {
    return (
      <B4InlineEdit
        value={device.alias || device.vendor || ""}
        onSave={onSaveAlias}
        onCancel={onCancelEdit}
      />
    );
  }

  return (
    <div className="flex items-center gap-1">
      {displayName ? (
        <Badge variant={isSelected ? "default" : "outline"}>
          {displayName}
        </Badge>
      ) : (
        <span className="text-muted-foreground text-xs">Unknown</span>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="sm"
            variant="ghost"
            onClick={onStartEdit}
            className="size-6 p-0 opacity-60 hover:opacity-100"
          >
            <EditIcon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          <p>Edit name</p>
        </TooltipContent>
      </Tooltip>
      {device.alias && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => void onResetAlias()}
              className="size-6 p-0 opacity-60 hover:opacity-100"
            >
              <RestoreIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Reset to vendor name</p>
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  );
};

export const DevicesSettings = ({ config, onChange }: DevicesSettingsProps) => {
  const [editingMac, setEditingMac] = useState<string | null>(null);

  const selectedMacs = config.queue.devices?.mac || [];
  const enabled = config.queue.devices?.enabled || false;
  const wisb = config.queue.devices?.wisb || false;
  const {
    devices,
    loading,
    available,
    source,
    loadDevices,
    setAlias,
    resetAlias,
  } = useDevices();

  useEffect(() => {
    void loadDevices();
  }, [loadDevices]);

  const handleMacToggle = (mac: string) => {
    const current = [...selectedMacs];
    const index = current.indexOf(mac);
    if (index === -1) {
      current.push(mac);
    } else {
      current.splice(index, 1);
    }
    onChange("queue.devices.mac", current);
  };

  const isSelected = (mac: string) => selectedMacs.includes(mac);
  const allSelected =
    devices.length > 0 && selectedMacs.length === devices.length;
  const someSelected =
    selectedMacs.length > 0 && selectedMacs.length < devices.length;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <DeviceUnknowIcon />
          <CardTitle>Device Filtering</CardTitle>
        </div>
        <CardDescription>
          Filter traffic by source device MAC address
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-4">
          <div className="grid grid-cols-2 items-start gap-6">
            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>Enable Device Filtering</FieldTitle>
                <FieldDescription>
                  Only process traffic from selected devices
                </FieldDescription>
              </FieldContent>
              <Switch
                checked={enabled}
                onCheckedChange={(checked) =>
                  onChange("queue.devices.enabled", checked)
                }
              />
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>Invert Selection (Blacklist)</FieldTitle>
                <FieldDescription>
                  {wisb
                    ? "Block selected devices"
                    : "Allow only selected devices"}
                </FieldDescription>
              </FieldContent>
              <Switch
                checked={wisb}
                onCheckedChange={(checked) =>
                  onChange("queue.devices.wisb", checked)
                }
                disabled={!enabled}
              />
            </Field>
          </div>

          {enabled && (
            <>
              <Alert variant={wisb ? "destructive" : "default"}>
                <AlertDescription>
                  {wisb
                    ? "Blacklist mode: Selected devices will be EXCLUDED from DPI bypass"
                    : "Whitelist mode: Only selected devices will use DPI bypass"}
                </AlertDescription>
              </Alert>

              {!available ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    DHCP lease source not detected. Device discovery
                    unavailable.
                  </AlertDescription>
                </Alert>
              ) : (
                <div className="col-span-1">
                  <div className="mb-2 flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <h6 className="text-sm font-semibold">
                        Available Devices
                      </h6>
                      {source && <Badge variant="secondary">{source}</Badge>}
                    </div>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => void loadDevices()}
                        >
                          {loading ? <Spinner /> : <RefreshIcon />}
                        </Button>
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>Refresh devices</p>
                      </TooltipContent>
                    </Tooltip>
                  </div>

                  <div className="bg-card border-border max-h-75 overflow-auto rounded-md border">
                    <table className="w-full border-collapse">
                      <thead className="bg-card sticky top-0 z-1">
                        <tr>
                          <th className="bg-card px-4 py-2 text-left">
                            <div className="relative">
                              <Checkbox
                                checked={allSelected || someSelected}
                                onCheckedChange={(checked) =>
                                  onChange(
                                    "queue.devices.mac",
                                    checked ? devices.map((d) => d.mac) : [],
                                  )
                                }
                                className={cn(
                                  someSelected && !allSelected && "opacity-50",
                                )}
                              />
                              {someSelected && !allSelected && (
                                <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
                                  <div className="bg-primary h-1.5 w-1.5 rounded-sm" />
                                </div>
                              )}
                            </div>
                          </th>
                          {["MAC Address", "IP", "Name"].map((label) => (
                            <th
                              key={label}
                              className="bg-card text-muted-foreground px-4 py-2 text-left font-semibold"
                            >
                              {label}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {devices.length === 0 ? (
                          <tr>
                            <td
                              colSpan={4}
                              className="text-muted-foreground py-8 text-center"
                            >
                              {loading
                                ? "Loading devices..."
                                : "No devices found"}
                            </td>
                          </tr>
                        ) : (
                          devices.map((device) => (
                            <tr
                              key={device.mac}
                              onClick={() => handleMacToggle(device.mac)}
                              className="hover:bg-muted cursor-pointer transition-colors"
                            >
                              <td
                                className="px-4 py-2"
                                onClick={(e) => e.stopPropagation()}
                              >
                                <Checkbox
                                  checked={isSelected(device.mac)}
                                  onCheckedChange={() =>
                                    handleMacToggle(device.mac)
                                  }
                                />
                              </td>
                              <td className="px-4 py-2 font-mono text-xs">
                                {device.mac}
                              </td>
                              <td className="px-4 py-2 font-mono text-xs">
                                {device.ip}
                              </td>
                              <td
                                className="px-4 py-2"
                                onClick={(e) => e.stopPropagation()}
                              >
                                <DeviceNameCell
                                  device={device}
                                  isSelected={isSelected(device.mac)}
                                  isEditing={editingMac === device.mac}
                                  onStartEdit={() => setEditingMac(device.mac)}
                                  onSaveAlias={async (alias) => {
                                    const result = await setAlias(
                                      device.mac,
                                      alias,
                                    );
                                    if (result.success) setEditingMac(null);
                                  }}
                                  onResetAlias={async () => {
                                    const result = await resetAlias(device.mac);
                                    if (result.success) setEditingMac(null);
                                  }}
                                  onCancelEdit={() => setEditingMac(null)}
                                />
                              </td>
                            </tr>
                          ))
                        )}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

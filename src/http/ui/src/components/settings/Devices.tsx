import { DeviceInfo, DevicesSettingsProps, useDevices } from "@b4.devices";
import {
  DeviceUnknowIcon,
  EditIcon,
  RefreshIcon,
  RestoreIcon,
} from "@b4.icons";
import { B4InlineEdit } from "@common/B4InlineEdit";
import { Separator } from "@design/primitives/separator";
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
  FieldGroup,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import { Spinner } from "@primitives/spinner";
import { Switch } from "@primitives/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@primitives/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
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
        <TooltipTrigger>
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
          <TooltipTrigger>
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

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <DeviceUnknowIcon />
          Device Filtering
        </CardTitle>

        <CardDescription>
          Filter traffic by source device MAC address
        </CardDescription>
      </CardHeader>
      <Separator />
      <CardContent>
        <FieldGroup>
          <div className="flex flex-row gap-4">
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
        </FieldGroup>

        {enabled && (
          <>
            <Separator className="my-4" />

            <FieldSet>
              <div className="flex justify-between">
                <FieldLegend>
                  Available Devices{" "}
                  {source && <Badge variant="secondary">{source}</Badge>}
                </FieldLegend>

                <Tooltip>
                  <TooltipTrigger>
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
              {!available ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    DHCP lease source not detected. Device discovery
                    unavailable.
                  </AlertDescription>
                </Alert>
              ) : (
                <div className="bg-card border-border max-h-75 overflow-auto border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>
                          <Checkbox
                            checked={allSelected}
                            onCheckedChange={(checked) =>
                              onChange(
                                "queue.devices.mac",
                                checked ? devices.map((d) => d.mac) : [],
                              )
                            }
                          />
                        </TableHead>
                        {["MAC Address", "IP", "Name"].map((label) => (
                          <TableHead key={label}>{label}</TableHead>
                        ))}
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {devices.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={4}
                            className="text-muted-foreground py-8 text-center"
                          >
                            {loading
                              ? "Loading devices..."
                              : "No devices found"}
                          </TableCell>
                        </TableRow>
                      ) : (
                        devices.map((device) => (
                          <TableRow key={device.mac}>
                            <TableCell>
                              <Checkbox
                                checked={isSelected(device.mac)}
                                onCheckedChange={() =>
                                  handleMacToggle(device.mac)
                                }
                              />
                            </TableCell>
                            <TableCell>{device.mac}</TableCell>
                            <TableCell>{device.ip}</TableCell>
                            <TableCell>
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
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              )}
            </FieldSet>
          </>
        )}
      </CardContent>
    </Card>
  );
};

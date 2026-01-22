import { ToggleOnIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Checkbox } from "@primitives/checkbox";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";
import { B4Config } from "@models/config";
import { Separator } from "@primitives/separator";
import { Label } from "@primitives/label";

interface FeatureSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: boolean | string | number | string[],
  ) => void;
}

export const FeatureSettings = ({ config, onChange }: FeatureSettingsProps) => {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <ToggleOnIcon />
          <CardTitle>Feature Flags</CardTitle>
        </div>
        <CardDescription>Enable or disable advanced features</CardDescription>
      </CardHeader>
      <Separator />
      <CardContent>
        <FieldGroup>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Enable IPv4 Support</FieldTitle>
              <FieldDescription>
                Whether to proccess IPv4 traffic
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-queue-ipv4"
              checked={config.queue.ipv4}
              onCheckedChange={(checked: boolean) =>
                onChange("queue.ipv4", checked)
              }
            />
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Enable IPv6 Support</FieldTitle>
              <FieldDescription>
                Whether to proccess IPv6 traffic
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-queue-ipv6"
              checked={config.queue.ipv6}
              onCheckedChange={(checked: boolean) =>
                onChange("queue.ipv6", checked)
              }
            />
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Skip IPTables/NFTables Setup</FieldTitle>
              <FieldDescription>
                Skip automatic IPTables/NFTables rules configuration
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-system-tables-skip-setup"
              checked={config.system.tables.skip_setup}
              onCheckedChange={(checked: boolean) =>
                onChange("system.tables.skip_setup", checked)
              }
            />
          </Field>

          <Field>
            <div className="flex justify-between">
              <FieldTitle>Firewall Monitor Interval</FieldTitle>
              <Badge variant="secondary" className="font-semibold">
                {config.system.tables.monitor_interval || "off"}
              </Badge>
            </div>
            <Slider
              value={[config.system.tables.monitor_interval]}
              onValueChange={(values) =>
                onChange(
                  "system.tables.monitor_interval",
                  Array.isArray(values) ? values[0] : values,
                )
              }
              min={0}
              max={120}
              step={5}
            />
            <FieldDescription>
              Interval for monitoring B4 iptables/nftables rules (default 10s)
            </FieldDescription>
          </Field>
        </FieldGroup>

        <Separator className="my-4" />

        <FieldSet>
          <FieldLegend>Network Interfaces</FieldLegend>
          <FieldDescription>
            Select interfaces to monitor (empty = all interfaces)
          </FieldDescription>
          <FieldGroup className="flex flex-col">
            {config.available_ifaces.map((iface) => {
              const isSelected = (config.queue.interfaces || []).includes(
                iface,
              );
              return (
                <Field key={iface} orientation="horizontal">
                  <Checkbox
                    id={`interface-${iface}`}
                    checked={isSelected}
                    onCheckedChange={(checked) => {
                      const current = config.queue.interfaces || [];
                      const updated = checked
                        ? [...current, iface]
                        : current.filter((i) => i !== iface);
                      onChange("queue.interfaces", updated);
                    }}
                  />
                  <FieldLabel
                    htmlFor={`interface-${iface}`}
                    className="font-normal"
                  >
                    {iface}
                  </FieldLabel>
                </Field>
              );
            })}
            {config.available_ifaces.length === 0 && (
              <Alert variant="destructive" className="mt-4">
                <AlertDescription>No interfaces detected</AlertDescription>
              </Alert>
            )}
            {config.queue.interfaces?.length === 0 && (
              <Alert className="mt-4">
                <AlertDescription>
                  B4 will listen on all available interfaces if none are
                  selected.
                </AlertDescription>
              </Alert>
            )}
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
};

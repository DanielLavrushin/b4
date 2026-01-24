import { ToggleOnIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
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
  FieldLabel,
  FieldSeparator,
  FieldTitle,
} from "@primitives/field";
import { Separator } from "@primitives/separator";
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";

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
        <CardTitle className="flex items-center gap-2">
          <ToggleOnIcon />
          Feature Flags
        </CardTitle>

        <CardDescription>Enable or disable advanced features</CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        <FieldGroup>
          <div className="flex flex-row gap-4">
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
          </div>
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
            <FieldLabel>
              Firewall Monitor Interval
              <Badge variant="secondary" className="ml-auto">
                {config.system.tables.monitor_interval || "off"}
              </Badge>
            </FieldLabel>
            <Slider
              value={[config.system.tables.monitor_interval]}
              onValueChange={(values) => {
                const [value] = values as [number];
                onChange("system.tables.monitor_interval", value);
              }}
              min={0}
              max={120}
              step={5}
            />
            <FieldDescription>
              Interval for monitoring B4 iptables/nftables rules (default 10s)
            </FieldDescription>
          </Field>
        </FieldGroup>

        <FieldSeparator className="my-6">Network Interfaces</FieldSeparator>

        <FieldGroup>
          {config.available_ifaces.map((iface) => {
            const isSelected = (config.queue.interfaces || []).includes(iface);
            return (
              <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4">
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
              </div>
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
                B4 will listen on all available interfaces if none are selected.
              </AlertDescription>
            </Alert>
          )}
        </FieldGroup>
      </CardContent>
    </Card>
  );
};

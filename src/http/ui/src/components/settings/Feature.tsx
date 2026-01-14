import { ToggleOnIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
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
} from "@design/components/ui/field";
import { Slider } from "@design/components/ui/slider";
import { Switch } from "@design/components/ui/switch";
import { B4Config } from "@models/config";
import { Separator } from "@design/components/ui/separator";
import { Label } from "@design/components/ui/label";

interface FeatureSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: boolean | string | number | string[],
  ) => void;
}

export const FeatureSettings = ({ config, onChange }: FeatureSettingsProps) => {
  const handleInterfaceToggle = (iface: string) => {
    const current = config.queue.interfaces || [];
    const updated = current.includes(iface)
      ? current.filter((i) => i !== iface)
      : [...current, iface];
    onChange("queue.interfaces", updated);
  };

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
                Whether to procces IPv4 traffic
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
                Whether to procces IPv6 traffic
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
                {config.system.tables.monitor_interval}
              </Badge>
            </div>
            <Slider
              value={[config.system.tables.monitor_interval]}
              onValueChange={(values) =>
                onChange("system.tables.monitor_interval", values[0])
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
          <FieldGroup>
            <div className="grid grid-cols-1 gap-4">
              <div>
                <div className="flex flex-wrap gap-2">
                  {config.available_ifaces.map((iface) => {
                    const isSelected = (config.queue.interfaces || []).includes(
                      iface,
                    );
                    return (
                      <Badge
                        key={iface}
                        variant={isSelected ? "default" : "outline"}
                        className="cursor-pointer"
                        onClick={() => handleInterfaceToggle(iface)}
                      >
                        {iface}
                      </Badge>
                    );
                  })}
                </div>
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
              </div>
            </div>
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
};

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
  FieldLabel,
  FieldTitle,
} from "@design/components/ui/field";
import { Slider } from "@design/components/ui/slider";
import { Switch } from "@design/components/ui/switch";
import { B4Config } from "@models/config";

interface FeatureSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: boolean | string | number | string[]
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
          <ToggleOnIcon className="h-5 w-5" />
          <CardTitle>Feature Flags</CardTitle>
        </div>
        <CardDescription>Enable or disable advanced features</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="mb-4">
          <FieldLabel className="text-base font-semibold mb-4">
            Proto Features
          </FieldLabel>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <label htmlFor="switch-queue-ipv4">
              <Field
                orientation="horizontal"
                className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2"
              >
                <FieldContent>
                  <FieldTitle>Enable IPv4 Support</FieldTitle>
                  <FieldDescription>Enable IPv4 support</FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-queue-ipv4"
                  checked={config.queue.ipv4}
                  onCheckedChange={(checked: boolean) =>
                    onChange("queue.ipv4", checked)
                  }
                />
              </Field>
            </label>
            <label htmlFor="switch-queue-ipv6">
              <Field
                orientation="horizontal"
                className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2"
              >
                <FieldContent>
                  <FieldTitle>Enable IPv6 Support</FieldTitle>
                  <FieldDescription>Enable IPv6 support</FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-queue-ipv6"
                  checked={config.queue.ipv6}
                  onCheckedChange={(checked: boolean) =>
                    onChange("queue.ipv6", checked)
                  }
                />
              </Field>
            </label>
          </div>
        </div>
        <div className="mb-4">
          <FieldLabel className="text-base font-semibold mb-4">
            Firewall Features
          </FieldLabel>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <label htmlFor="switch-system-tables-skip-setup">
              <Field
                orientation="horizontal"
                className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2"
              >
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
            </label>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Firewall Monitor Interval in seconds (default 10s)
                </FieldLabel>
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
                className="w-full"
              />
              <FieldDescription>
                Interval for monitoring B4 iptables/nftables rules
              </FieldDescription>
              {config.system.tables.monitor_interval <= 0 && (
                <div className="mt-2">
                  <Alert variant="destructive">
                    <AlertDescription>
                      Warning: This <strong>disables</strong> automatic
                      monitoring of B4 iptables/nftables
                    </AlertDescription>
                  </Alert>
                </div>
              )}
            </Field>
          </div>
        </div>
        <div className="mb-4">
          <FieldLabel className="text-base font-semibold mb-4">
            Network Interfaces
          </FieldLabel>
          <div className="grid grid-cols-1 gap-4">
            <div>
              <p className="text-sm text-muted-foreground mb-2">
                Select interfaces to monitor (empty = all interfaces)
              </p>
              <div className="flex flex-wrap gap-2">
                {config.available_ifaces.map((iface) => {
                  const isSelected = (config.queue.interfaces || []).includes(
                    iface
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
        </div>
      </CardContent>
    </Card>
  );
};

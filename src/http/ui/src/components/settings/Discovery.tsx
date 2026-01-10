import { AddIcon, DiscoveryIcon } from "@b4.icons";
import { ChipList } from "@components/common/ChipList";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Separator } from "@design/components/ui/separator";
import { Slider } from "@design/components/ui/slider";
import { B4Config } from "@models/config";
import { useState } from "react";

interface CheckerSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: string | boolean | number | string[]
  ) => void;
}

export const CheckerSettings = ({ config, onChange }: CheckerSettingsProps) => {
  const [newDns, setNewDns] = useState("");

  const handleAddDns = () => {
    if (newDns.trim()) {
      const current = config.system.checker.reference_dns || [];
      if (!current.includes(newDns.trim())) {
        onChange("system.checker.reference_dns", [...current, newDns.trim()]);
      }
      setNewDns("");
    }
  };

  const handleRemoveDns = (dns: string) => {
    const current = config.system.checker.reference_dns || [];
    onChange(
      "system.checker.reference_dns",
      current.filter((s) => s !== dns)
    );
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <DiscoveryIcon className="h-5 w-5" />
          <CardTitle>Testing Configuration</CardTitle>
        </div>
        <CardDescription>Configure testing behavior and output</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Field className="w-full space-y-2">
            <div className="flex items-center justify-between">
              <FieldLabel className="text-sm font-medium">
                Discovery Timeout
              </FieldLabel>
              <Badge variant="secondary" className="font-semibold">
                {config.system.checker.discovery_timeout || 5} sec
              </Badge>
            </div>
            <Slider
              value={[config.system.checker.discovery_timeout || 5]}
              onValueChange={(values) =>
                onChange("system.checker.discovery_timeout", values[0])
              }
              min={3}
              max={30}
              step={1}
              className="w-full"
            />
            <FieldDescription>
              Timeout per preset during discovery
            </FieldDescription>
          </Field>
          <Field className="w-full space-y-2">
            <div className="flex items-center justify-between">
              <FieldLabel className="text-sm font-medium">
                Config Propagation Delay
              </FieldLabel>
              <Badge variant="secondary" className="font-semibold">
                {config.system.checker.config_propagate_ms || 1500} ms
              </Badge>
            </div>
            <Slider
              value={[config.system.checker.config_propagate_ms || 1500]}
              onValueChange={(values) =>
                onChange("system.checker.config_propagate_ms", values[0])
              }
              min={500}
              max={5000}
              step={100}
              className="w-full"
            />
            <FieldDescription>
              Delay for config to propagate to workers (increase on slow
              devices)
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel>Reference Domain</FieldLabel>
            <Input
              value={config.system.checker.reference_domain || "yandex.ru"}
              onChange={(e) =>
                onChange("system.checker.reference_domain", e.target.value)
              }
              placeholder="yandex.ru"
            />
            <FieldDescription>
              Fast domain to measure your network baseline speed
            </FieldDescription>
          </Field>

          <div className="relative my-4 lg:col-span-2 flex items-center">
            <Separator className="absolute inset-0 top-1/2" />
            <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
              DNS Configuration
            </span>
          </div>
          <div className="col-span-1 md:col-span-2">
            <div className="flex flex-col gap-1.5">
              <FieldLabel>Add DNS Server</FieldLabel>
              <div className="flex gap-2 items-start">
                <Input
                  value={newDns}
                  onChange={(e) => setNewDns(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      handleAddDns();
                    }
                  }}
                  placeholder="e.g., 8.8.8.8"
                  className="flex-1"
                />
                <Button
                  variant="secondary"
                  size="icon"
                  onClick={handleAddDns}
                  disabled={!newDns.trim()}
                >
                  <AddIcon className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
          <div className="col-span-1 md:col-span-2">
            <ChipList
              items={config.system.checker.reference_dns || []}
              getKey={(d) => d}
              getLabel={(d) => d}
              onDelete={handleRemoveDns}
              title="Active DNS servers to test:"
            />
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

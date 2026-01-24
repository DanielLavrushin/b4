import { DiscoveryIcon } from "@b4.icons";
import TagsInput from "@design/composed/tags-input";
import { B4Config } from "@models/config";
import { Badge } from "@primitives/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSeparator,
} from "@primitives/field";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";
import { Slider } from "@primitives/slider";
import { useState } from "react";

interface CheckerSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
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
      current.filter((s) => s !== dns),
    );
  };

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <DiscoveryIcon />
            Testing Configuration
          </CardTitle>
          <CardDescription>
            Configure testing behavior and output
          </CardDescription>
        </CardHeader>

        <Separator />

        <CardContent>
          <FieldGroup>
            <div className="flex flex-row gap-4">
              <Field>
                <FieldLabel>
                  Discovery Timeout
                  <Badge variant="secondary" className="ml-auto">
                    {config.system.checker.discovery_timeout || 5} sec
                  </Badge>
                </FieldLabel>
                <Slider
                  value={[config.system.checker.discovery_timeout || 5]}
                  onValueChange={([value]) =>
                    onChange("system.checker.discovery_timeout", value)
                  }
                  min={3}
                  max={30}
                  step={1}
                />
                <FieldDescription>
                  Timeout per preset during discovery
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel>
                  Config Propagation Delay
                  <Badge variant="secondary" className="ml-auto">
                    {config.system.checker.config_propagate_ms || 1500} ms
                  </Badge>
                </FieldLabel>
                <Slider
                  value={[config.system.checker.config_propagate_ms || 1500]}
                  onValueChange={([value]) =>
                    onChange("system.checker.config_propagate_ms", value)
                  }
                  min={500}
                  max={5000}
                  step={100}
                />
                <FieldDescription>
                  Delay for config to propagate to workers (increase on slow
                  devices)
                </FieldDescription>
              </Field>
            </div>

            <Field>
              <FieldLabel>Reference Domain</FieldLabel>
              <Input
                value={config.system.checker.reference_domain || "yandex.ru"}
                onChange={(e) =>
                  onChange("system.checker.reference_domain", e.target.value)
                }
                placeholder="max.ru"
              />
              <FieldDescription>
                Fast domain to measure your network baseline speed
              </FieldDescription>
            </Field>
          </FieldGroup>
          <FieldSeparator className="my-6">DNS Configuration</FieldSeparator>

          <Field>
            <FieldLabel>Add DNS Server</FieldLabel>
            <TagsInput
              value={config.system.checker.reference_dns || []}
              onValueChange={(values: string[]) =>
                onChange("system.checker.reference_dns", values)
              }
              placeholder="e.g., 8.8.8.8"
            />
            <FieldDescription>DNS servers to test</FieldDescription>
          </Field>
        </CardContent>
      </Card>
    </div>
  );
};

import { NetworkIcon } from "@b4.icons";
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
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";
import { Slider } from "@primitives/slider";
import { B4Config } from "@models/config";

interface NetworkSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

export const NetworkSettings = ({ config, onChange }: NetworkSettingsProps) => (
  <Card>
    <CardHeader>
      <div className="flex items-center gap-2">
        <NetworkIcon />
        <CardTitle>Network Configuration</CardTitle>
      </div>
      <CardDescription>
        Configure netfilter queue and network processing parameters
      </CardDescription>
    </CardHeader>
    <Separator />
    <CardContent>
      <FieldSet>
        <FieldLegend>Queue Settings</FieldLegend>
        <FieldGroup>
          <Field>
            <FieldLabel>Queue Start Number</FieldLabel>
            <Input
              type="number"
              value={config.queue.start_num}
              onChange={(e) =>
                onChange("queue.start_num", Number(e.target.value))
              }
            />
            <FieldDescription>
              Netfilter queue number (0-65535)
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel>Packet Mark</FieldLabel>
            <Input
              type="number"
              value={config.queue.mark || 32768}
              onChange={(e) => onChange("queue.mark", Number(e.target.value))}
            />
            <FieldDescription>
              Netfilter packet mark for iptables rules (default: 32768)
            </FieldDescription>
          </Field>
          <Field className="md:col-span-2">

            <FieldLabel>Worker Threads
              <Badge variant="secondary" className="font-semibold">
                {config.queue.threads}
              </Badge>
            </FieldLabel>
            
            <Slider
              value={[config.queue.threads]}
              onValueChange={(values) => onChange("queue.threads", values[0])}
              min={1}
              max={16}
              step={1}
            />
            <FieldDescription>
              Number of worker threads for processing packets simultaneously
              (default 4)
            </FieldDescription>
          </Field>
        </FieldGroup>
      </FieldSet>

      <Separator className="my-6" />

      <FieldSet>
        <FieldLegend>Web Server</FieldLegend>
        <FieldGroup>
          <Field>
            <FieldLabel>Bind Address</FieldLabel>
            <Input
              value={config.system.web_server.bind_address || "0.0.0.0"}
              onChange={(e) =>
                onChange("system.web_server.bind_address", e.target.value)
              }
              placeholder="0.0.0.0"
            />
            <FieldDescription>
              IP to bind (0.0.0.0 = all, 127.0.0.1 = localhost only, :: = all
              IPv6)
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel>Port</FieldLabel>
            <Input
              type="number"
              value={config.system.web_server.port}
              onChange={(e) =>
                onChange("system.web_server.port", Number(e.target.value))
              }
            />
            <FieldDescription>Web Server port (default: 7000)</FieldDescription>
          </Field>
        </FieldGroup>
      </FieldSet>
    </CardContent>
  </Card>
);

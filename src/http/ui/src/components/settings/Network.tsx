import { NetworkIcon } from "@b4.icons";
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
      <CardTitle className="flex items-center gap-2">
        <NetworkIcon />
        Network Configuration
      </CardTitle>

      <CardDescription>
        Configure netfilter queue and network processing parameters
      </CardDescription>
    </CardHeader>
    <Separator />
    <CardContent>
      <FieldGroup>
        <div className="flex flex-row gap-4">
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
              Netfilter packet mark (default: 32768)
            </FieldDescription>
          </Field>
        </div>

        <Field>
          <FieldLabel>
            Worker Threads
            <Badge variant="secondary" className="ml-auto">
              {config.queue.threads}
            </Badge>
          </FieldLabel>

          <Slider
            value={[config.queue.threads]}
            onValueChange={(values) =>
              onChange(
                "queue.threads",
                Array.isArray(values) ? values[0] : values,
              )
            }
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
      <FieldSeparator className="my-6">Web Server</FieldSeparator>

      <FieldGroup className="flex-row gap-4">
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
    </CardContent>
  </Card>
);

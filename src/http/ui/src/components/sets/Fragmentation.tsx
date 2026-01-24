import { B4SetConfig, FragmentationStrategy } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
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
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@primitives/select";
import { Separator } from "@primitives/separator";
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";
import { ComboSettings } from "./frags/Combo";
import { DisorderSettings } from "./frags/Disorder";
import { ExtSplitSettings } from "./frags/ExtSplit";
import { FirstByteSettings } from "./frags/FirstByte";
import { TcpIpSettings } from "./frags/TcpIp";

interface FragmentationSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

const fragmentationOptions: { label: string; value: FragmentationStrategy }[] =
  [
    { label: "Combo", value: "combo" },
    { label: "Hybrid", value: "hybrid" },
    { label: "Disorder", value: "disorder" },
    { label: "Extension Split", value: "extsplit" },
    { label: "First-Byte Desync", value: "firstbyte" },
    { label: "TCP Segmentation", value: "tcp" },
    { label: "IP Fragmentation", value: "ip" },
    { label: "TLS Record Splitting", value: "tls" },
    { label: "OOB (Out-of-Band)", value: "oob" },
    { label: "Disabled", value: "none" },
  ];

export const FragmentationSettings = ({
  config,
  onChange,
}: FragmentationSettingsProps) => {
  const strategy = config.fragmentation.strategy;
  const isTcpOrIp = strategy === "tcp" || strategy === "ip";
  const isOob = strategy === "oob";
  const isTls = strategy === "tls";
  const isActive = strategy !== "none";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Fragmentation Strategy</CardTitle>
        <CardDescription>
          Split packets to evade DPI pattern matching
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        {/* Strategy Selection */}
        <FieldGroup>
          <Field>
            <FieldLabel>Method</FieldLabel>
            <Select
              value={strategy}
              onValueChange={(value) =>
                onChange("fragmentation.strategy", value as string)
              }
            >
              <SelectTrigger>
                <SelectValue placeholder="Select fragmentation method" />
              </SelectTrigger>
              <SelectContent>
                {fragmentationOptions.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value.toString()}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Reverse Fragment Order</FieldTitle>
              <FieldDescription>Send second fragment first</FieldDescription>
            </FieldContent>
            <Switch
              id="switch-fragmentation-reverse-order"
              checked={config.fragmentation.reverse_order}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.reverse_order", checked)
              }
            />
          </Field>
        </FieldGroup>

        <Separator className="my-4" />

        {isTcpOrIp && <TcpIpSettings config={config} onChange={onChange} />}

        {strategy === "combo" && (
          <ComboSettings config={config} onChange={onChange} />
        )}

        {strategy === "disorder" && (
          <DisorderSettings config={config} onChange={onChange} />
        )}
        {strategy === "extsplit" && <ExtSplitSettings />}

        {strategy === "firstbyte" && <FirstByteSettings config={config} />}

        {isOob && (
          <FieldSet>
            <FieldLegend>OOB (Out-of-Band) Strategy</FieldLegend>
            <FieldDescription>
              Inserts a byte with TCP URG flag. Server ignores it, but stateful
              DPI gets confused.
            </FieldDescription>

            <Field>
              <FieldLabel>
                Insert Position
                <Badge variant="secondary">
                  {config.fragmentation.oob_position || 1}
                </Badge>
              </FieldLabel>
              <Slider
                value={[config.fragmentation.oob_position || 1]}
                onValueChange={([value]) =>
                  onChange("fragmentation.oob_position", value)
                }
                min={1}
                max={50}
                step={1}
              />
              <FieldDescription>Bytes before OOB insertion</FieldDescription>
            </Field>
            <Field>
              <FieldDescription>
                OOB Byte:{" "}
                <code className="font-mono text-xs">
                  {String.fromCharCode(config.fragmentation.oob_char || 120)}
                </code>{" "}
                (0x
                {(config.fragmentation.oob_char || 120)
                  .toString(16)
                  .padStart(2, "0")}
                )
              </FieldDescription>
            </Field>
          </FieldSet>
        )}

        {/* TLS Record Settings */}
        {isTls && (
          <FieldSet>
            <FieldLegend>TLS Record Split Position</FieldLegend>
            <FieldDescription>
              Splits ClientHello into multiple TLS records. DPI expecting
              single-record handshake fails to match.
            </FieldDescription>

            <Field>
              <FieldLabel>
                Record Split Position
                <Badge variant="secondary">
                  {config.fragmentation.tlsrec_pos || 1}
                </Badge>
              </FieldLabel>
              <Slider
                value={[config.fragmentation.tlsrec_pos || 1]}
                onValueChange={([value]) =>
                  onChange("fragmentation.tlsrec_pos", value)
                }
                min={1}
                max={100}
                step={1}
                className="w-full"
              />
              <FieldDescription>
                First TLS record size in bytes
              </FieldDescription>
            </Field>
          </FieldSet>
        )}

        {!isActive && (
          <Alert variant="destructive" className="md:col-span-2">
            <AlertDescription>
              Fragmentation disabled. Only fake packets (if enabled) will be
              used for bypass.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
};

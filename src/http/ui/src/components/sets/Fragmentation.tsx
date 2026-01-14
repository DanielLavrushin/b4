import { FragIcon } from "@b4.icons";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@design/components/ui/select";
import { Separator } from "@design/components/ui/separator";
import { Slider } from "@design/components/ui/slider";
import { Switch } from "@design/components/ui/switch";
import { B4SetConfig, FragmentationStrategy } from "@models/config";
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
        <div className="flex items-center gap-3">
          <div className="bg-accent text-accent-foreground flex h-10 w-10 items-center justify-center rounded-md">
            <FragIcon />
          </div>
          <div className="flex-1">
            <CardTitle>Fragmentation Strategy</CardTitle>
            <CardDescription className="mt-1">
              Split packets to evade DPI pattern matching
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          {/* Strategy Selection */}
          <div>
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
          </div>

          <div>
            <label htmlFor="switch-fragmentation-reverse-order">
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>Reverse Fragment Order</FieldTitle>
                  <FieldDescription>
                    Send second fragment first
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-fragmentation-reverse-order"
                  checked={config.fragmentation.reverse_order}
                  onCheckedChange={(checked: boolean) =>
                    onChange("fragmentation.reverse_order", checked)
                  }
                />
              </Field>
            </label>
          </div>

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
            <>
              <div className="relative my-4 flex items-center md:col-span-2">
                <Separator className="absolute inset-0 top-1/2" />
                <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
                  OOB (Out-of-Band) Strategy
                </span>
              </div>

              <Alert className="md:col-span-2">
                <AlertDescription>
                  Inserts a byte with TCP URG flag. Server ignores it, but
                  stateful DPI gets confused.
                </AlertDescription>
              </Alert>

              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Insert Position
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.fragmentation.oob_position || 1}
                    </Badge>
                  </div>
                  <Slider
                    value={[config.fragmentation.oob_position || 1]}
                    onValueChange={(values) =>
                      onChange("fragmentation.oob_position", values[0])
                    }
                    min={1}
                    max={50}
                    step={1}
                    className="w-full"
                  />
                  <FieldDescription>
                    Bytes before OOB insertion
                  </FieldDescription>
                </Field>
              </div>

              <div>
                <div>
                  <p className="mb-2 text-sm">
                    OOB Byte:{" "}
                    <code className="bg-muted rounded px-1 py-0.5 font-mono text-xs">
                      {String.fromCharCode(
                        config.fragmentation.oob_char || 120,
                      )}
                    </code>{" "}
                    (0x
                    {(config.fragmentation.oob_char || 120)
                      .toString(16)
                      .padStart(2, "0")}
                    )
                  </p>
                </div>
              </div>
            </>
          )}

          {/* TLS Record Settings */}
          {isTls && (
            <>
              <div className="relative my-4 flex items-center md:col-span-2">
                <Separator className="absolute inset-0 top-1/2" />
                <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
                  TLS Record Splitting Strategy
                </span>
              </div>

              <Alert className="md:col-span-2">
                <AlertDescription>
                  Splits ClientHello into multiple TLS records. DPI expecting
                  single-record handshake fails to match.
                </AlertDescription>
              </Alert>

              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Record Split Position
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.fragmentation.tlsrec_pos || 1}
                    </Badge>
                  </div>
                  <Slider
                    value={[config.fragmentation.tlsrec_pos || 1]}
                    onValueChange={(values) =>
                      onChange("fragmentation.tlsrec_pos", values[0])
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
              </div>
            </>
          )}

          {!isActive && (
            <Alert variant="destructive" className="md:col-span-2">
              <AlertDescription>
                Fragmentation disabled. Only fake packets (if enabled) will be
                used for bypass.
              </AlertDescription>
            </Alert>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

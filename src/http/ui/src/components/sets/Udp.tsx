import { WarningIcon, UdpIcon } from "@b4.icons";
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
import { Input } from "@design/components/ui/input";
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
import { B4SetConfig } from "@models/config";

interface UdpSettingsProps {
  config: B4SetConfig;
  main: B4SetConfig;
  onChange: (field: string, value: string | boolean | number) => void;
}

const UDP_MODES = [
  {
    value: "drop",
    label: "Drop",
    description: "Drop matched UDP packets (forces TCP fallback)",
  },
  {
    value: "fake",
    label: "Fake & Fragment",
    description: "Send fake packets and fragment real ones (DPI bypass)",
  },
];

const UDP_QUIC_FILTERS = [
  {
    value: "disabled",
    label: "Disabled",
    description: "Don't process QUIC at all",
  },
  {
    value: "all",
    label: "All QUIC",
    description: "Match all QUIC Initial packets (blind matching)",
  },
  {
    value: "parse",
    label: "Parse SNI",
    description: "Match only QUIC with SNI in domain list (smart matching)",
  },
];

const UDP_FAKING_STRATEGIES = [
  { value: "none", label: "None", description: "No faking strategy" },
  {
    value: "ttl",
    label: "TTL",
    description: "Use low TTL to make packets expire",
  },
  { value: "checksum", label: "Checksum", description: "Corrupt UDP checksum" },
];

export const UdpSettings = ({ config, main, onChange }: UdpSettingsProps) => {
  const isQuicEnabled = config.udp.filter_quic !== "disabled";
  const hasPortFilter =
    config.udp.dport_filter && config.udp.dport_filter.trim() !== "";
  const hasDomainsConfigured =
    config.targets?.sni_domains?.length > 0 ||
    config.targets?.geosite_categories?.length > 0;

  const willProcessUdp = isQuicEnabled || hasPortFilter;

  const showActionSettings = willProcessUdp;

  const isFakeMode = config.udp.mode === "fake";
  const showFakeSettings = showActionSettings && isFakeMode;

  const showParseWarning =
    config.udp.filter_quic === "parse" && !hasDomainsConfigured;
  const showNoProcessingWarning = !willProcessUdp;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
            <UdpIcon />
          </div>
          <div className="flex-1">
            <CardTitle>UDP & QUIC Configuration</CardTitle>
            <CardDescription className="mt-1">
              Configure UDP packet processing and QUIC filtering
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div className="relative my-4 md:col-span-2 flex items-center">
            <Separator className="absolute inset-0 top-1/2" />
            <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
              What UDP Traffic to Process
            </span>
          </div>

          <div>
            <Field>
              <FieldLabel>QUIC Filter</FieldLabel>
              <Select
                value={config.udp.filter_quic}
                onValueChange={(value) =>
                  onChange("udp.filter_quic", value as string)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select QUIC filter" />
                </SelectTrigger>
                <SelectContent>
                  {UDP_QUIC_FILTERS.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value.toString()}
                    >
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {
                  UDP_QUIC_FILTERS.find(
                    (o) => o.value === config.udp.filter_quic
                  )?.description
                }
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field>
              <FieldLabel>Port Filter</FieldLabel>
              <Input
                value={config.udp.dport_filter}
                onChange={(e) => onChange("udp.dport_filter", e.target.value)}
                placeholder="e.g., 5000-6000,8000"
              />
              <FieldDescription>
                Match specific UDP ports (VoIP, gaming, etc.) - leave empty to
                disable
              </FieldDescription>
            </Field>
          </div>

          {/* STUN Filter */}
          <div>
            <label htmlFor="switch-udp-filter-stun">
              <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
                <FieldContent>
                  <FieldTitle>Filter STUN Packets</FieldTitle>
                  <FieldDescription>
                    Ignore STUN packets (recommended for voice/video calls)
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-udp-filter-stun"
                  checked={config.udp.filter_stun}
                  onCheckedChange={(checked) =>
                    onChange("udp.filter_stun", checked)
                  }
                />
              </Field>
            </label>
          </div>

          {/* Parse mode warning */}
          {showParseWarning && (
            <Alert variant="destructive">
              <WarningIcon className="h-3.5 w-3.5" />
              <AlertDescription>
                <strong>Parse mode requires domains:</strong> Add domains in the
                Targets section for SNI matching to work. Without domains, no
                QUIC traffic will be processed.
              </AlertDescription>
            </Alert>
          )}

          {/* No processing warning */}
          {showNoProcessingWarning && (
            <Alert>
              <AlertDescription>
                <strong>UDP processing disabled:</strong> Enable QUIC filtering
                or add port filters to process UDP traffic. Currently, all UDP
                packets will pass through unchanged.
              </AlertDescription>
            </Alert>
          )}

          {/* Section 2: Action Settings (only if traffic will be processed) */}
          {showActionSettings && (
            <>
              <div className="relative my-4 md:col-span-2 flex items-center">
                <Separator className="absolute inset-0 top-1/2" />
                <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                  How to Handle Matched Traffic
                </span>
              </div>

              {/* UDP Mode */}
              <div>
                <Field>
                  <FieldLabel>Action Mode</FieldLabel>
                  <Select
                    value={config.udp.mode}
                    onValueChange={(value) =>
                      onChange("udp.mode", value as string)
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select action mode" />
                    </SelectTrigger>
                    <SelectContent>
                      {UDP_MODES.map((option) => (
                        <SelectItem
                          key={option.value}
                          value={option.value.toString()}
                        >
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {
                      UDP_MODES.find((o) => o.value === config.udp.mode)
                        ?.description
                    }
                  </FieldDescription>
                </Field>
              </div>

              {/* Connection Packets Limit */}
              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Connection Packets Limit
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.udp.conn_bytes_limit}
                    </Badge>
                  </div>
                  <Slider
                    value={[config.udp.conn_bytes_limit]}
                    onValueChange={(values) =>
                      onChange("udp.conn_bytes_limit", values[0])
                    }
                    min={1}
                    max={main.id === config.id ? 30 : main.udp.conn_bytes_limit}
                    step={1}
                    className="w-full"
                  />
                  <FieldDescription>
                    {main.id === config.id
                      ? "Main set limit (changing requires service restart to take effect)"
                      : `Max: ${main.udp.conn_bytes_limit} (limited by main set)`}
                  </FieldDescription>
                </Field>
              </div>

              {/* Info about current mode */}
              <Alert>
                <AlertDescription>
                  {isFakeMode ? (
                    <>
                      <strong>Fake mode:</strong> Matched UDP packets will be
                      preceded by fake packets and fragmented to bypass DPI
                      systems. Configure fake packet settings below.
                    </>
                  ) : (
                    <>
                      <strong>Drop mode:</strong> Matched UDP packets will be
                      dropped, forcing the application to fall back to TCP
                      (e.g., QUIC → HTTPS).
                    </>
                  )}
                </AlertDescription>
              </Alert>
            </>
          )}

          {/* Section 3: Fake Mode Settings (only if fake mode is enabled) */}
          {showFakeSettings && (
            <>
              <div className="relative my-4 md:col-span-2 flex items-center">
                <Separator className="absolute inset-0 top-1/2" />
                <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                  Fake Packet Configuration
                </span>
              </div>

              <div>
                <Field>
                  <FieldLabel>Faking Strategy</FieldLabel>
                  <Select
                    value={config.udp.faking_strategy}
                    onValueChange={(value) =>
                      onChange("udp.faking_strategy", value as string)
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select faking strategy" />
                    </SelectTrigger>
                    <SelectContent>
                      {UDP_FAKING_STRATEGIES.map((option) => (
                        <SelectItem
                          key={option.value}
                          value={option.value.toString()}
                        >
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {
                      UDP_FAKING_STRATEGIES.find(
                        (o) => o.value === config.udp.faking_strategy
                      )?.description
                    }
                  </FieldDescription>
                </Field>
              </div>

              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Fake Packet Count
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.udp.fake_seq_length}
                    </Badge>
                  </div>
                  <Slider
                    value={[config.udp.fake_seq_length]}
                    onValueChange={(values) =>
                      onChange("udp.fake_seq_length", values[0])
                    }
                    min={1}
                    max={20}
                    step={1}
                    className="w-full"
                  />
                  <FieldDescription>
                    Number of fake packets sent before real packet
                  </FieldDescription>
                </Field>
              </div>

              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Fake Packet Size
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.udp.fake_len} bytes
                    </Badge>
                  </div>
                  <Slider
                    value={[config.udp.fake_len]}
                    onValueChange={(values) =>
                      onChange("udp.fake_len", values[0])
                    }
                    min={32}
                    max={1500}
                    step={8}
                    className="w-full"
                  />
                  <FieldDescription>
                    Size of each fake UDP packet payload
                  </FieldDescription>
                </Field>
              </div>
              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      Segment 2 Delay
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.udp.seg2delay} ms
                    </Badge>
                  </div>
                  <Slider
                    value={[config.udp.seg2delay]}
                    onValueChange={(values) =>
                      onChange("udp.seg2delay", values[0])
                    }
                    min={0}
                    max={1000}
                    step={10}
                    className="w-full"
                  />
                  <FieldDescription>Delay between segments</FieldDescription>
                </Field>
              </div>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

import { TagsInput } from "@composed/tags-input";
import { B4SetConfig } from "@models/config";
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
  FieldSeparator,
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
import { parsePortFilter, validatePortFilter } from "@utils";

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

  const willProcessUdp = isQuicEnabled || hasPortFilter;

  const showActionSettings = willProcessUdp;

  const isFakeMode = config.udp.mode === "fake";
  const showFakeSettings = showActionSettings && isFakeMode;

  const handlePortFilterChange = (values: string[]) => {
    onChange("udp.dport_filter", validatePortFilter(values));
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>UDP & QUIC Configuration</CardTitle>
        <CardDescription>
          Configure UDP packet processing and QUIC filtering
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        <FieldGroup>
          <Field>
            <FieldLabel>QUIC Filter</FieldLabel>
            <Select
              value={config.udp.filter_quic}
              onValueChange={(value) =>
                onChange("udp.filter_quic", value as string)
              }
              items={UDP_QUIC_FILTERS}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select QUIC filter" />
              </SelectTrigger>
              <SelectContent>
                {UDP_QUIC_FILTERS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {
                UDP_QUIC_FILTERS.find((o) => o.value === config.udp.filter_quic)
                  ?.description
              }
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel>Port Filter</FieldLabel>
            <TagsInput
              value={parsePortFilter(config.udp.dport_filter)}
              onValueChange={handlePortFilterChange}
              placeholder="1-65535 or 1000-2000"
            />
            <FieldDescription>
              Match specific UDP ports (VoIP, gaming, etc.) - leave empty to
              disable
            </FieldDescription>
          </Field>

          <Field orientation="horizontal">
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
        </FieldGroup>

        {/* Section 2: Action Settings (only if traffic will be processed) */}
        {showActionSettings && (
          <>
            <FieldSeparator className="my-6">
              How to Handle Matched Traffic
            </FieldSeparator>

            <FieldGroup>
              <div className="flex flex-row gap-4">
                <Field>
                  <FieldLabel>Action Mode</FieldLabel>
                  <Select
                    value={config.udp.mode}
                    onValueChange={(value) =>
                      onChange("udp.mode", value as string)
                    }
                    items={UDP_MODES}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select action mode" />
                    </SelectTrigger>
                    <SelectContent>
                      {UDP_MODES.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
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

                <Field>
                  <FieldLabel>
                    Connection Packets Limit
                    <Badge variant="secondary" className="ml-auto">
                      {config.udp.conn_bytes_limit}
                    </Badge>
                  </FieldLabel>

                  <Slider
                    value={[config.udp.conn_bytes_limit]}
                    onValueChange={(val) =>
                      onChange("udp.conn_bytes_limit", val as number)
                    }
                    min={1}
                    max={main.id === config.id ? 30 : main.udp.conn_bytes_limit}
                    step={1}
                  />
                  <FieldDescription>
                    {main.id === config.id
                      ? "Main set limit (changing requires service restart to take effect)"
                      : `Max: ${main.udp.conn_bytes_limit} (limited by main set)`}
                  </FieldDescription>
                </Field>
              </div>
            </FieldGroup>
          </>
        )}

        {/* Section 3: Fake Mode Settings (only if fake mode is enabled) */}
        {showFakeSettings && (
          <>
            <FieldSeparator className="my-6">
              Fake Packet Configuration
            </FieldSeparator>

            <FieldGroup>
              <div className="grid grid-cols-2 gap-4">
                <Field>
                  <FieldLabel>Faking Strategy</FieldLabel>
                  <Select
                    value={config.udp.faking_strategy}
                    onValueChange={(value) =>
                      onChange("udp.faking_strategy", value as string)
                    }
                    items={UDP_FAKING_STRATEGIES}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select faking strategy" />
                    </SelectTrigger>
                    <SelectContent>
                      {UDP_FAKING_STRATEGIES.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {
                      UDP_FAKING_STRATEGIES.find(
                        (o) => o.value === config.udp.faking_strategy,
                      )?.description
                    }
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel>
                    Fake Packet Count
                    <Badge variant="secondary" className="ml-auto">
                      {config.udp.fake_seq_length}
                    </Badge>
                  </FieldLabel>

                  <Slider
                    value={[config.udp.fake_seq_length]}
                    onValueChange={(val) =>
                      onChange("udp.fake_seq_length", val as number)
                    }
                    min={1}
                    max={20}
                    step={1}
                  />
                  <FieldDescription>
                    Number of fake packets sent before real packet
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel>
                    Fake Packet Size
                    <Badge variant="secondary" className="ml-auto">
                      {config.udp.fake_len} bytes
                    </Badge>
                  </FieldLabel>

                  <Slider
                    value={[config.udp.fake_len]}
                    onValueChange={(val) =>
                      onChange("udp.fake_len", val as number)
                    }
                    min={32}
                    max={1500}
                    step={8}
                  />
                  <FieldDescription>
                    Size of each fake UDP packet payload
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel>
                    Segment 2 Delay
                    <Badge variant="secondary" className="ml-auto">
                      {config.udp.seg2delay} ms
                    </Badge>
                  </FieldLabel>

                  <Slider
                    value={[config.udp.seg2delay]}
                    onValueChange={(val) =>
                      onChange("udp.seg2delay", val as number)
                    }
                    min={0}
                    max={1000}
                    step={10}
                  />
                  <FieldDescription>Delay between segments</FieldDescription>
                </Field>
              </div>
            </FieldGroup>
          </>
        )}
      </CardContent>
    </Card>
  );
};

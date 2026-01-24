import { TagsInput } from "@composed/tags-input";
import {
  B4SetConfig,
  DesyncMode,
  IncomingMode,
  IncomingStrategy,
  WindowMode,
} from "@models/config";
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

interface TcpSettingsProps {
  config: B4SetConfig;
  main: B4SetConfig;
  onChange: (
    field: string,
    value: string | number | boolean | number[],
  ) => void;
}

const desyncModeOptions: { label: string; value: DesyncMode }[] = [
  { label: "Disabled", value: "off" },
  { label: "RST Packets", value: "rst" },
  { label: "FIN Packets", value: "fin" },
  { label: "ACK Packets", value: "ack" },
  { label: "Combo (RST + FIN)", value: "combo" },
  { label: "Full (RST + FIN + ACK)", value: "full" },
];

const desyncModeDescriptions: Record<DesyncMode, string> = {
  off: "No desynchronization - packets sent normally",
  rst: "Inject fake RST packets with bad checksums to disrupt DPI state tracking",
  fin: "Inject fake FIN packets with past sequence numbers to confuse connection state",
  ack: "Inject fake ACK packets with random future sequence/ack numbers",
  combo: "Send RST + FIN + ACK sequence for stronger desync effect",
  full: "Full attack: fake SYN, overlapping RSTs, PSH, and URG packets",
};

const windowModeOptions: { label: string; value: WindowMode }[] = [
  { label: "Disabled", value: "off" },
  { label: "Zero Window", value: "zero" },
  { label: "Random Window", value: "random" },
  { label: "Oscillate", value: "oscillate" },
  { label: "Escalate", value: "escalate" },
];

const windowModeDescriptions: Record<WindowMode, string> = {
  off: "No window manipulation - use actual TCP window",
  zero: "Send fake packets: first with window=0, then window=65535",
  random: "Send 3-5 fake packets with random window sizes from your list",
  oscillate: "Cycle through your custom window values sequentially",
  escalate: "Gradually increase: 0 → 100 → 500 → 1460 → 8192 → 32768 → 65535",
};

const incomingModeOptions: { label: string; value: IncomingMode }[] = [
  { label: "Disabled", value: "off" },
  { label: "Fake Packets", value: "fake" },
  { label: "Reset Injection", value: "reset" },
  { label: "FIN Injection", value: "fin" },
  { label: "Desync Combo", value: "desync" },
];

const incomingModeDescriptions: Record<IncomingMode, string> = {
  off: "No incoming packet manipulation",
  fake: "Inject corrupted ACK packets toward server with low TTL on every incoming data packet",
  reset: "Inject fake RST packets when incoming bytes threshold reached",
  fin: "Inject fake FIN packets when incoming bytes threshold reached",
  desync: "Inject RST+FIN+ACK combo when incoming bytes threshold reached",
};

const incomingStrategyOptions: {
  label: string;
  value: IncomingStrategy;
}[] = [
  { label: "Bad Checksum", value: "badsum" },
  { label: "Bad Sequence", value: "badseq" },
  { label: "Bad ACK", value: "badack" },
  { label: "Random", value: "rand" },
  { label: "All Corruptions", value: "all" },
];

const incomingStrategyDescriptions: Record<IncomingStrategy, string> = {
  badsum: "Corrupt TCP checksum only - packets dropped by kernel",
  badseq: "Corrupt sequence number - packets ignored by TCP stack",
  badack: "Corrupt ACK number - packets ignored by TCP stack",
  rand: "Randomly pick corruption method per packet",
  all: "Apply all corruptions: bad seq + bad ack + bad checksum",
};

export const TcpSettings = ({ config, main, onChange }: TcpSettingsProps) => {
  const winValues = config.tcp.win.values || [0, 1460, 8192, 65535];
  const showWinValues = ["oscillate", "random"].includes(config.tcp.win.mode);
  const isDesyncEnabled = config.tcp.desync.mode !== "off";

  const handleWinValuesChange = (values: string[]) => {
    // Валидация: только числа от 0 до 65535, уникальные
    const validNumbers = values
      .map((v) => parseInt(v.trim(), 10))
      .filter((v) => !isNaN(v) && v >= 0 && v <= 65535)
      .filter((v, index, arr) => arr.indexOf(v) === index); // Уникальные

    onChange(
      "tcp.win.values",
      validNumbers.sort((a, b) => a - b),
    );
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>TCP Configuration</CardTitle>
        <CardDescription>
          Configure TCP packet handling and DPI bypass techniques
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        {/* Basic TCP Settings */}
        <FieldSet>
          <FieldGroup>
            <Field>
              <FieldContent>
                <FieldLabel>
                  Connection Bytes Limit
                  <Badge variant="secondary">
                    {config.tcp.conn_bytes_limit}
                  </Badge>
                </FieldLabel>
              </FieldContent>
              <Slider
                value={[config.tcp.conn_bytes_limit]}
                onValueChange={([value]) =>
                  onChange("tcp.conn_bytes_limit", value)
                }
                min={1}
                max={main.id === config.id ? 100 : main.tcp.conn_bytes_limit}
                step={1}
              />
              <FieldDescription>
                {main.id === config.id
                  ? "Main set limit (changing requires service restart to take effect)"
                  : `Max: ${main.tcp.conn_bytes_limit} (limited by main set)`}
              </FieldDescription>
            </Field>
            <Field>
              <FieldContent>
                <FieldLabel>
                  Segment 2 Delay
                  <Badge variant="secondary">{config.tcp.seg2delay} ms</Badge>
                </FieldLabel>
              </FieldContent>
              <Slider
                value={[config.tcp.seg2delay]}
                onValueChange={([value]) => onChange("tcp.seg2delay", value)}
                min={0}
                max={1000}
                step={10}
              />
              <FieldDescription>
                Delay between TCP segments (helps with timing-based DPI)
              </FieldDescription>
            </Field>

            {/* SACK and SYN Fake */}

            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>Drop SACK Options</FieldTitle>
                <FieldDescription>
                  Strip Selective Acknowledgment from TCP headers to confuse
                  stateful DPI
                </FieldDescription>
              </FieldContent>
              <Switch
                id="switch-tcp-drop-sack"
                checked={config.tcp.drop_sack || false}
                onCheckedChange={(checked) =>
                  onChange("tcp.drop_sack", checked)
                }
              />
            </Field>

            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>SYN Fake Packets</FieldTitle>
                <FieldDescription>
                  Send fake SYN packets during handshake (aggressive technique)
                </FieldDescription>
              </FieldContent>
              <Switch
                id="switch-tcp-syn-fake"
                checked={config.tcp.syn_fake || false}
                onCheckedChange={(checked) => onChange("tcp.syn_fake", checked)}
              />
            </Field>

            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>SYN MD5 Signature</FieldTitle>
                <FieldDescription>
                  Send fake SYN with TCP MD5 option before real handshake
                </FieldDescription>
              </FieldContent>
              <Switch
                id="switch-tcp-syn-md5"
                checked={config.faking.tcp_md5 || false}
                onCheckedChange={(checked) =>
                  onChange("faking.tcp_md5", checked)
                }
              />
            </Field>

            {config.tcp.syn_fake && (
              <FieldGroup>
                <Field>
                  <FieldContent>
                    <FieldLabel>
                      SYN Fake Payload Length
                      <Badge variant="secondary">
                        {config.tcp.syn_fake_len || 0} bytes
                      </Badge>
                    </FieldLabel>
                  </FieldContent>
                  <Slider
                    value={[config.tcp.syn_fake_len || 0]}
                    onValueChange={([value]) =>
                      onChange("tcp.syn_fake_len", value)
                    }
                    min={0}
                    max={1200}
                    step={64}
                  />
                  <FieldDescription>
                    0 = header only, {">"}0 = add fake TLS payload
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldContent>
                    <FieldLabel>
                      SYN Fake TTL
                      <Badge variant="secondary">
                        {config.tcp.syn_ttl || 0}
                      </Badge>
                    </FieldLabel>
                  </FieldContent>
                  <Slider
                    value={[config.tcp.syn_ttl || 0]}
                    onValueChange={([value]) => onChange("tcp.syn_ttl", value)}
                    min={1}
                    max={100}
                    step={1}
                  />
                  <FieldDescription>
                    TTL value for SYN fake packets (default 3 if unset)
                  </FieldDescription>
                </Field>
              </FieldGroup>
            )}
          </FieldGroup>
        </FieldSet>
        <Separator className="my-4" />
        {/* TCP Window Configuration */}
        <FieldSet>
          <FieldLegend className="flex items-center gap-2">
            TCP Window Manipulation
          </FieldLegend>
          <FieldDescription>
            Window manipulation sends fake ACK packets with modified TCP window
            sizes before your real packet. These fakes use low TTL so they
            expire before reaching the server but confuse middlebox DPI.
          </FieldDescription>

          <FieldGroup>
            <Field>
              <FieldLabel>Window Mode</FieldLabel>
              <Select
                value={config.tcp.win.mode}
                onValueChange={(value) =>
                  onChange("tcp.win.mode", value as string)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select window mode" />
                </SelectTrigger>
                <SelectContent>
                  {windowModeOptions.map((option) => (
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
                {windowModeDescriptions[config.tcp.win.mode]}
              </FieldDescription>
            </Field>

            {showWinValues && (
              <Field>
                <FieldLabel>Custom Window Values</FieldLabel>
                <TagsInput
                  value={winValues.map((v) => v.toString())}
                  onValueChange={handleWinValuesChange}
                  placeholder="0-65535"
                />
                <FieldDescription>
                  {config.tcp.win.mode === "oscillate"
                    ? "Packets will cycle through these values in order"
                    : "Random values will be picked from this list"}
                </FieldDescription>
              </Field>
            )}
          </FieldGroup>
        </FieldSet>

        <Separator className="my-4" />

        {/* TCP Desync Configuration */}
        <FieldSet>
          <FieldLegend className="flex items-center gap-2">
            TCP Desync Attack
          </FieldLegend>
          <FieldDescription>
            Desync attacks inject fake TCP control packets (RST/FIN/ACK) with
            corrupted checksums and low TTL. These packets confuse stateful DPI
            systems but are discarded by the real server.
          </FieldDescription>

          <FieldGroup>
            <Field>
              <FieldLabel>Desync Mode</FieldLabel>
              <Select
                value={config.tcp.desync.mode}
                onValueChange={(value) =>
                  onChange("tcp.desync.mode", value as string)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select desync mode" />
                </SelectTrigger>
                <SelectContent>
                  {desyncModeOptions.map((option) => (
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
                {desyncModeDescriptions[config.tcp.desync.mode]}
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel>
                Desync Packet Count
                <Badge variant="secondary">{config.tcp.desync.count}</Badge>
              </FieldLabel>
              <Slider
                value={[config.tcp.desync.count]}
                onValueChange={([value]) => onChange("tcp.desync.count", value)}
                min={1}
                max={20}
                step={1}
                disabled={!isDesyncEnabled}
              />
              <FieldDescription>
                {isDesyncEnabled
                  ? "Number of fake packets per desync attack"
                  : "Enable desync mode first"}
              </FieldDescription>
            </Field>

            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>Post-ClientHello RST</FieldTitle>
                <FieldDescription>
                  Send fake RST after ClientHello to evict connection from DPI
                  tracking table
                </FieldDescription>
              </FieldContent>
              <Switch
                id="switch-tcp-post-desync"
                checked={config.tcp.desync.post_desync || false}
                onCheckedChange={(checked) =>
                  onChange("tcp.desync.post_desync", checked)
                }
              />
            </Field>

            <Field>
              <FieldLabel>
                Desync TTL
                <Badge variant="secondary">{config.tcp.desync.ttl}</Badge>
              </FieldLabel>
              <Slider
                value={[config.tcp.desync.ttl]}
                onValueChange={([value]) => onChange("tcp.desync.ttl", value)}
                min={1}
                max={50}
                step={1}
                disabled={!isDesyncEnabled}
              />
              <FieldDescription>
                {isDesyncEnabled
                  ? "Low TTL ensures packets expire before reaching server"
                  : "Enable desync mode first"}
              </FieldDescription>
            </Field>
          </FieldGroup>
        </FieldSet>

        <Separator className="my-4" />
        {/* Incoming Response Manipulation */}
        <FieldSet>
          <FieldLegend className="flex items-center gap-2">
            Incoming Response Bypass
          </FieldLegend>
          <FieldDescription>
            Manipulates incoming server responses to bypass DPI that throttles
            connections after receiving ~15-20KB. Experimental feature for
            TSPU-style behavioral throttling.
          </FieldDescription>

          <FieldGroup>
            <Field>
              <FieldLabel>Incoming Mode</FieldLabel>
              <Select
                value={config.tcp.incoming?.mode || "off"}
                onValueChange={(value) =>
                  onChange("tcp.incoming.mode", value as string)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select incoming mode" />
                </SelectTrigger>
                <SelectContent>
                  {incomingModeOptions.map((option) => (
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
                {incomingModeDescriptions[config.tcp.incoming?.mode || "off"]}
              </FieldDescription>
            </Field>

            {config.tcp.incoming?.mode !== "off" && (
              <>
                <Field>
                  <FieldLabel>Corruption Strategy</FieldLabel>
                  <Select
                    value={config.tcp.incoming?.strategy || "badsum"}
                    onValueChange={(value) =>
                      onChange("tcp.incoming.strategy", value as IncomingStrategy)
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select strategy" />
                    </SelectTrigger>
                    <SelectContent>
                      {incomingStrategyOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {
                      incomingStrategyDescriptions[
                        (config.tcp.incoming?.strategy || "badsum")
                      ]
                    }
                  </FieldDescription>
                </Field>

                {config.tcp.incoming?.mode !== "fake" && (
                  <>
                    <Field>
                      <FieldLabel>
                        Threshold Min
                        <Badge variant="secondary">
                          {config.tcp.incoming?.min || 14} KB
                        </Badge>
                      </FieldLabel>
                      <Slider
                        value={[config.tcp.incoming?.min || 14]}
                        onValueChange={([value]) =>
                          onChange("tcp.incoming.min", value)
                        }
                        min={5}
                        max={config.tcp.incoming?.max || 50}
                        step={1}
                      />
                      <FieldDescription>
                        Minimum threshold for injection trigger
                      </FieldDescription>
                    </Field>

                    <Field>
                      <FieldLabel>
                        Threshold Max
                        <Badge variant="secondary">
                          {config.tcp.incoming?.max || 14} KB
                        </Badge>
                      </FieldLabel>
                      <Slider
                        value={[config.tcp.incoming?.max || 14]}
                        onValueChange={([value]) =>
                          onChange("tcp.incoming.max", value)
                        }
                        min={config.tcp.incoming?.min || 5}
                        max={50}
                        step={1}
                      />
                      <FieldDescription>
                        {config.tcp.incoming?.min === config.tcp.incoming?.max
                          ? "Fixed threshold (min = max)"
                          : "Threshold randomized between min-max per connection"}
                      </FieldDescription>
                    </Field>
                  </>
                )}

                <Field>
                  <FieldLabel>
                    Fake TTL
                    <Badge variant="secondary">
                      {config.tcp.incoming?.fake_ttl || 3}
                    </Badge>
                  </FieldLabel>
                  <Slider
                    value={[config.tcp.incoming?.fake_ttl || 3]}
                    onValueChange={([value]) =>
                      onChange("tcp.incoming.fake_ttl", value)
                    }
                    min={1}
                    max={20}
                    step={1}
                  />
                  <FieldDescription>
                    Low TTL ensures fakes expire before reaching server
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel>
                    Fake Count
                    <Badge variant="secondary">
                      {config.tcp.incoming?.fake_count || 3}
                    </Badge>
                  </FieldLabel>
                  <Slider
                    value={[config.tcp.incoming?.fake_count || 3]}
                    onValueChange={([value]) =>
                      onChange("tcp.incoming.fake_count", value)
                    }
                    min={1}
                    max={10}
                    step={1}
                  />
                  <FieldDescription>
                    Number of fake packets per injection
                  </FieldDescription>
                </Field>
              </>
            )}
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
};

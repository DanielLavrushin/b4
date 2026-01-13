import { AddIcon, TcpIcon } from "@b4.icons";
import { ChipList } from "@components/common/ChipList";
import { Alert, AlertDescription } from "@design/components/ui/alert";
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
import {
  B4SetConfig,
  DesyncMode,
  WindowMode,
  IncomingMode,
  IncomingStrategy,
} from "@models/config";
import { useState } from "react";

interface TcpSettingsProps {
  config: B4SetConfig;
  main: B4SetConfig;
  onChange: (
    field: string,
    value: string | number | boolean | number[]
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

const incomingStrategyOptions: { label: string; value: string }[] = [
  { label: "Bad Checksum", value: "badsum" },
  { label: "Bad Sequence", value: "badseq" },
  { label: "Bad ACK", value: "badack" },
  { label: "Random", value: "rand" },
  { label: "All Corruptions", value: "all" },
];

const incomingStrategyDescriptions: Record<string, string> = {
  badsum: "Corrupt TCP checksum only - packets dropped by kernel",
  badseq: "Corrupt sequence number - packets ignored by TCP stack",
  badack: "Corrupt ACK number - packets ignored by TCP stack",
  rand: "Randomly pick corruption method per packet",
  all: "Apply all corruptions: bad seq + bad ack + bad checksum",
};

export const TcpSettings = ({ config, main, onChange }: TcpSettingsProps) => {
  const [newWinValue, setNewWinValue] = useState("");

  const winValues = config.tcp.win.values || [0, 1460, 8192, 65535];
  const showWinValues = ["oscillate", "random"].includes(config.tcp.win.mode);
  const isDesyncEnabled = config.tcp.desync.mode !== "off";

  const handleAddWinValue = () => {
    const val = parseInt(newWinValue, 10);
    if (!isNaN(val) && val >= 0 && val <= 65535 && !winValues.includes(val)) {
      onChange(
        "tcp.win.values",
        [...winValues, val].sort((a, b) => a - b)
      );
      setNewWinValue("");
    }
  };

  const handleRemoveWinValue = (val: number) => {
    onChange(
      "tcp.win.values",
      winValues.filter((v) => v !== val)
    );
  };

  return (
    <Card className="flex flex-col">
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
            <TcpIcon />
          </div>
          <div className="flex-1">
            <CardTitle>TCP Configuration</CardTitle>
            <CardDescription className="mt-1">
              Configure TCP packet handling and DPI bypass techniques
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <Separator className="mb-4" />
      <CardContent className="flex flex-col gap-4">
        {/* Basic TCP Settings */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Connection Bytes Limit
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.conn_bytes_limit}
                </Badge>
              </div>
              <Slider
                value={[config.tcp.conn_bytes_limit]}
                onValueChange={(values) =>
                  onChange("tcp.conn_bytes_limit", values[0])
                }
                min={1}
                max={main.id === config.id ? 100 : main.tcp.conn_bytes_limit}
                step={1}
                className="w-full"
              />
              <FieldDescription>
                {main.id === config.id
                  ? "Main set limit (changing requires service restart to take effect)"
                  : `Max: ${main.tcp.conn_bytes_limit} (limited by main set)`}
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
                  {config.tcp.seg2delay} ms
                </Badge>
              </div>
              <Slider
                value={[config.tcp.seg2delay]}
                onValueChange={(values) => onChange("tcp.seg2delay", values[0])}
                min={0}
                max={1000}
                step={10}
                className="w-full"
              />
              <FieldDescription>
                Delay between TCP segments (helps with timing-based DPI)
              </FieldDescription>
            </Field>
          </div>

          {/* SACK and SYN Fake */}
          <div>
            <label htmlFor="switch-tcp-drop-sack">
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
            </label>
          </div>

          <div>
            <label htmlFor="switch-tcp-syn-fake">
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>SYN Fake Packets</FieldTitle>
                  <FieldDescription>
                    Send fake SYN packets during handshake (aggressive
                    technique)
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-tcp-syn-fake"
                  checked={config.tcp.syn_fake || false}
                  onCheckedChange={(checked) =>
                    onChange("tcp.syn_fake", checked)
                  }
                />
              </Field>
            </label>
          </div>

          <div>
            <label htmlFor="switch-tcp-syn-md5">
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
            </label>
          </div>

          {config.tcp.syn_fake && (
            <>
              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      SYN Fake Payload Length
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.tcp.syn_fake_len || 0} bytes
                    </Badge>
                  </div>
                  <Slider
                    value={[config.tcp.syn_fake_len || 0]}
                    onValueChange={(values) =>
                      onChange("tcp.syn_fake_len", values[0])
                    }
                    min={0}
                    max={1200}
                    step={64}
                    className="w-full"
                  />
                  <FieldDescription>
                    0 = header only, {">"}0 = add fake TLS payload
                  </FieldDescription>
                </Field>
              </div>
              <div>
                <Field className="w-full space-y-2">
                  <div className="flex items-center justify-between">
                    <FieldLabel className="text-sm font-medium">
                      SYN Fake TTL
                    </FieldLabel>
                    <Badge variant="secondary" className="font-semibold">
                      {config.tcp.syn_ttl || 0}
                    </Badge>
                  </div>
                  <Slider
                    value={[config.tcp.syn_ttl || 0]}
                    onValueChange={(values) =>
                      onChange("tcp.syn_ttl", values[0])
                    }
                    min={1}
                    max={100}
                    step={1}
                    className="w-full"
                  />
                  <FieldDescription>
                    TTL value for SYN fake packets (default 3 if unset)
                  </FieldDescription>
                </Field>
              </div>
            </>
          )}
        </div>

        {/* TCP Window Configuration */}
        <div className="relative my-4 md:col-span-2 flex items-center">
          <Separator className="absolute inset-0 top-1/2" />
          <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
            TCP Window Manipulation
          </span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <Alert className="md:col-span-2">
            <AlertDescription>
              Window manipulation sends fake ACK packets with modified TCP
              window sizes before your real packet. These fakes use low TTL so
              they expire before reaching the server but confuse middlebox DPI.
            </AlertDescription>
          </Alert>

          <div>
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
          </div>

          {showWinValues && (
            <div className="md:col-span-2">
              <p className="text-sm font-semibold mb-1">Custom Window Values</p>
              <p className="text-xs text-muted-foreground mb-4">
                {config.tcp.win.mode === "oscillate"
                  ? "Packets will cycle through these values in order"
                  : "Random values will be picked from this list"}
              </p>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-center">
                <div className="flex gap-4 mt-2 items-center">
                  <Field className="flex-1">
                    <FieldLabel>Add Value (0-65535)</FieldLabel>
                    <Input
                      value={newWinValue}
                      onChange={(e) => setNewWinValue(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          handleAddWinValue();
                        }
                      }}
                      type="number"
                    />
                  </Field>

                  <Button
                    variant="secondary"
                    size="icon"
                    onClick={handleAddWinValue}
                    disabled={!newWinValue}
                  >
                    <AddIcon />
                  </Button>
                </div>
                <div>
                  <ChipList
                    items={winValues}
                    getKey={(v) => v}
                    getLabel={(v) => v.toLocaleString()}
                    onDelete={handleRemoveWinValue}
                    emptyMessage="No values configured - defaults will be used"
                    showEmpty
                  />
                </div>
              </div>
            </div>
          )}
        </div>

        {/* TCP Desync Configuration */}
        <div className="relative my-4 md:col-span-2 flex items-center">
          <Separator className="absolute inset-0 top-1/2" />
          <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
            TCP Desync Attack
          </span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <Alert className="md:col-span-3">
            <AlertDescription>
              Desync attacks inject fake TCP control packets (RST/FIN/ACK) with
              corrupted checksums and low TTL. These packets confuse stateful
              DPI systems but are discarded by the real server.
            </AlertDescription>
          </Alert>
          <div>
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
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Desync TTL
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.desync.ttl}
                </Badge>
              </div>
              <Slider
                value={[config.tcp.desync.ttl]}
                onValueChange={(values) =>
                  onChange("tcp.desync.ttl", values[0])
                }
                min={1}
                max={50}
                step={1}
                disabled={!isDesyncEnabled}
                className="w-full"
              />
              <FieldDescription>
                {isDesyncEnabled
                  ? "Low TTL ensures packets expire before reaching server"
                  : "Enable desync mode first"}
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Desync Packet Count
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.desync.count}
                </Badge>
              </div>
              <Slider
                value={[config.tcp.desync.count]}
                onValueChange={(values) =>
                  onChange("tcp.desync.count", values[0])
                }
                min={1}
                max={20}
                step={1}
                disabled={!isDesyncEnabled}
                className="w-full"
              />
              <FieldDescription>
                {isDesyncEnabled
                  ? "Number of fake packets per desync attack"
                  : "Enable desync mode first"}
              </FieldDescription>
            </Field>
          </div>

          <div>
            <label htmlFor="switch-tcp-post-desync">
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
            </label>
          </div>
        </div>

        {/* Incoming Response Manipulation */}
        <div className="relative my-4 md:col-span-2 flex items-center">
          <Separator className="absolute inset-0 top-1/2" />
          <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
            Incoming Response Bypass
          </span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <Alert className="md:col-span-2">
            <AlertDescription>
              Manipulates incoming server responses to bypass DPI that throttles
              connections after receiving ~15-20KB. Experimental feature for
              TSPU-style behavioral throttling.
            </AlertDescription>
          </Alert>

          <div>
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
          </div>

          <div>
            <Field>
              <FieldLabel>Corruption Strategy</FieldLabel>
              <Select
                value={config.tcp.incoming?.strategy || "badsum"}
                onValueChange={(value) =>
                  onChange("tcp.incoming.strategy", value as string)
                }
                disabled={config.tcp.incoming?.mode === "off"}
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
                {config.tcp.incoming?.mode === "off"
                  ? "Enable incoming mode first"
                  : incomingStrategyDescriptions[
                      config.tcp.incoming?.strategy || "badsum"
                    ]}
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Threshold Min
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.incoming?.min || 14} KB
                </Badge>
              </div>
              <Slider
                value={[config.tcp.incoming?.min || 14]}
                onValueChange={(values) =>
                  onChange("tcp.incoming.min", values[0])
                }
                min={5}
                max={config.tcp.incoming?.max || 50}
                step={1}
                disabled={
                  config.tcp.incoming?.mode === "off" ||
                  config.tcp.incoming?.mode === "fake"
                }
                className="w-full"
              />
              <FieldDescription>
                {config.tcp.incoming?.mode === "fake"
                  ? "Not used in fake mode (triggers on every packet)"
                  : "Minimum threshold for injection trigger"}
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Threshold Max
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.incoming?.max || 14} KB
                </Badge>
              </div>
              <Slider
                value={[config.tcp.incoming?.max || 14]}
                onValueChange={(values) =>
                  onChange("tcp.incoming.max", values[0])
                }
                min={config.tcp.incoming?.min || 5}
                max={50}
                step={1}
                disabled={
                  config.tcp.incoming?.mode === "off" ||
                  config.tcp.incoming?.mode === "fake"
                }
                className="w-full"
              />
              <FieldDescription>
                {config.tcp.incoming?.mode === "fake"
                  ? "Not used in fake mode"
                  : config.tcp.incoming?.min === config.tcp.incoming?.max
                  ? "Fixed threshold (min = max)"
                  : "Threshold randomized between min-max per connection"}
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Fake TTL
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.incoming?.fake_ttl || 3}
                </Badge>
              </div>
              <Slider
                value={[config.tcp.incoming?.fake_ttl || 3]}
                onValueChange={(values) =>
                  onChange("tcp.incoming.fake_ttl", values[0])
                }
                min={1}
                max={20}
                step={1}
                disabled={config.tcp.incoming?.mode === "off"}
                className="w-full"
              />
              <FieldDescription>
                Low TTL ensures fakes expire before reaching server
              </FieldDescription>
            </Field>
          </div>

          <div>
            <Field className="w-full space-y-2">
              <div className="flex items-center justify-between">
                <FieldLabel className="text-sm font-medium">
                  Fake Count
                </FieldLabel>
                <Badge variant="secondary" className="font-semibold">
                  {config.tcp.incoming?.fake_count || 3}
                </Badge>
              </div>
              <Slider
                value={[config.tcp.incoming?.fake_count || 3]}
                onValueChange={(values) =>
                  onChange("tcp.incoming.fake_count", values[0])
                }
                min={1}
                max={10}
                step={1}
                disabled={config.tcp.incoming?.mode === "off"}
                className="w-full"
              />
              <FieldDescription>
                Number of fake packets per injection
              </FieldDescription>
            </Field>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

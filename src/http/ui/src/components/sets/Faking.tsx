import { useCaptures } from "@b4.capture";
import { TagsInput } from "@composed/tags-input";
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
import { Input } from "@primitives/input";
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
import { Textarea } from "@primitives/textarea";
import { useEffect } from "react";
import { Link } from "react-router-dom";

import {
  B4SetConfig,
  FakingPayloadType,
  FakingStrategy,
  MutationMode,
} from "@models/config";

interface FakingSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

const FAKE_STRATEGIES: { value: FakingStrategy; label: string }[] = [
  { value: "ttl", label: "TTL" },
  { value: "randseq", label: "Random Sequence" },
  { value: "pastseq", label: "Past Sequence" },
  { value: "tcp_check", label: "TCP Check" },
  { value: "md5sum", label: "MD5 Sum" },
];

const FAKE_PAYLOAD_TYPES = [
  { value: 0, label: "Random" },
  // { value: 1, label: "Custom" },
  { value: 2, label: "Preset: Google (classic)" },
  { value: 3, label: "Preset: DuckDuckGo" },
  { value: 4, label: "My own Payload File" },
];

const MUTATION_MODES: { value: MutationMode; label: string }[] = [
  { value: "off", label: "Disabled" },
  { value: "random", label: "Random" },
  { value: "grease", label: "GREASE Extensions" },
  { value: "padding", label: "Padding" },
  { value: "fakeext", label: "Fake Extensions" },
  { value: "fakesni", label: "Fake SNIs" },
  { value: "advanced", label: "Advanced (All)" },
];

const mutationModeDescriptions: Record<MutationMode, string> = {
  off: "No ClientHello mutation applied",
  random: "Randomize extension order and add noise",
  grease: "Insert GREASE extensions to confuse DPI",
  padding: "Add padding extension to reach target size",
  fakeext: "Insert fake/unknown TLS extensions",
  fakesni: "Add additional fake SNI entries",
  advanced: "Combine multiple mutation techniques",
};

export const FakingSettings = ({ config, onChange }: FakingSettingsProps) => {
  const { captures, loadCaptures } = useCaptures();

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  const mutation = config.faking.sni_mutation || {
    mode: "off" as MutationMode,
    grease_count: 3,
    padding_size: 2048,
    fake_ext_count: 5,
    fake_snis: [],
  };

  const isMutationEnabled = mutation.mode !== "off";
  const showGreaseSettings = ["grease", "advanced"].includes(mutation.mode);
  const showPaddingSettings = ["padding", "advanced"].includes(mutation.mode);
  const showFakeExtSettings = ["fakeext", "advanced"].includes(mutation.mode);
  const showFakeSniSettings = ["fakesni", "advanced"].includes(mutation.mode);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Fake SNI Configuration</CardTitle>
        <CardDescription>
          Configure fake SNI packets to confuse DPI
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        <FieldSet>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Enable Fake SNI</FieldTitle>
              <FieldDescription>
                Send fake SNI packets before real ClientHello
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-faking-sni"
              checked={config.faking.sni}
              onCheckedChange={(checked: boolean) =>
                onChange("faking.sni", checked)
              }
            />
          </Field>

          <Field>
            <FieldLabel>Fake Strategy</FieldLabel>
            <Select
              value={config.faking.strategy}
              onValueChange={(value) =>
                onChange("faking.strategy", value as FakingStrategy)
              }
              disabled={!config.faking.sni}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select fake strategy" />
              </SelectTrigger>
              <SelectContent>
                {FAKE_STRATEGIES.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              How to make fake packets unprocessable by server
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel>Fake Payload Type</FieldLabel>
            <Select
              value={config.faking.sni_type?.toString()}
              onValueChange={(value) =>
                onChange("faking.sni_type", Number(value))
              }
              disabled={!config.faking.sni}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select payload type" />
              </SelectTrigger>
              <SelectContent>
                {FAKE_PAYLOAD_TYPES.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value.toString()}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>Content of fake packets</FieldDescription>
          </Field>

          {config.faking.sni_type === FakingPayloadType.CUSTOM && (
            <Field>
              <FieldLabel>Custom Payload (Hex)</FieldLabel>
              <Textarea
                value={config.faking.custom_payload}
                onChange={(e) =>
                  onChange("faking.custom_payload", e.target.value)
                }
                disabled={!config.faking.sni}
                rows={2}
              />
              <FieldDescription>
                Hex-encoded payload for fake packets (use Capture feature to get
                real payloads)
              </FieldDescription>
            </Field>
          )}

          {config.faking.sni_type === FakingPayloadType.CAPTURE && (
            <>
              {captures.length > 0 && (
                <Field>
                  <FieldLabel>Captured Payload</FieldLabel>
                  <Select
                    value={config.faking.payload_file ?? "none"}
                    onValueChange={(value) =>
                      onChange(
                        "faking.payload_file",
                        value === "none" ? "" : (value as string),
                      )
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select a capture..." />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">Select a capture...</SelectItem>
                      {captures.map((c) => (
                        <SelectItem key={c.filepath} value={c.filepath}>
                          {c.domain} ({c.size} bytes)
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {captures.length === 0
                      ? "No TLS captures available. Use Capture feature first."
                      : "Select a previously captured/uploaded TLS ClientHello"}
                  </FieldDescription>
                </Field>
              )}
              <Field>
                <Alert>
                  <AlertDescription>
                    {captures.length === 0 &&
                      "No TLS captures available. You can use the Capture feature to record ClientHello payloads or  upload your own capture files. "}

                    <Link to="/settings/capture">
                      Navigate to the Settings section to capture or upload your
                      own TLS ClientHello payloads.
                    </Link>
                  </AlertDescription>
                </Alert>
              </Field>
            </>
          )}

          <Field>
            <FieldLabel>
              Fake TTL
              <Badge variant="secondary" className="font-semibold">
                {config.faking.ttl}
              </Badge>
            </FieldLabel>
            <Slider
              value={[config.faking.ttl]}
              onValueChange={([value]: [number]) =>
                onChange("faking.ttl", value)
              }
              min={1}
              max={64}
              step={1}
              disabled={!config.faking.sni}
            />
            <FieldDescription>
              TTL for fake packets (should expire before server)
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel>Sequence Offset</FieldLabel>
            <Input
              type="number"
              value={config.faking.seq_offset}
              onChange={(e) =>
                onChange("faking.seq_offset", Number(e.target.value))
              }
              disabled={!config.faking.sni}
            />
            <FieldDescription>
              TCP sequence number offset for pastseq strategy
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel>
              Fake Packet Count
              <Badge variant="secondary">{config.faking.sni_seq_length}</Badge>
            </FieldLabel>
            <Slider
              value={[config.faking.sni_seq_length]}
              onValueChange={([value]: [number]) =>
                onChange("faking.sni_seq_length", value)
              }
              min={1}
              max={20}
              step={1}
              disabled={!config.faking.sni}
            />
            <FieldDescription>Number of fake packets to send</FieldDescription>
          </Field>
        </FieldSet>

        <Separator className="my-4" />

        {/* TLS Mod Options - only show when payload has TLS structure */}
        {config.faking.sni_type !== FakingPayloadType.RANDOM && (
          <FieldSet>
            <FieldLegend>Fake Packet TLS Modification</FieldLegend>
            <FieldDescription>
              Modify fake TLS ClientHello to improve bypass (zapret-style)
            </FieldDescription>
            <FieldGroup>
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>Randomize TLS Random</FieldTitle>
                  <FieldDescription>
                    Replace 32-byte Random field in fake packets
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-faking-tls-rnd"
                  checked={(config.faking.tls_mod || []).includes("rnd")}
                  onCheckedChange={(checked: boolean) => {
                    const current = config.faking.tls_mod || [];
                    const next = checked
                      ? [...current.filter((m) => m !== "rnd"), "rnd"]
                      : current.filter((m) => m !== "rnd");
                    onChange("faking.tls_mod", next);
                  }}
                  disabled={!config.faking.sni}
                />
              </Field>

              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>Duplicate Session ID</FieldTitle>
                  <FieldDescription>
                    Copy Session ID from real ClientHello into fake
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-faking-tls-dupsid"
                  checked={(config.faking.tls_mod || []).includes("dupsid")}
                  onCheckedChange={(checked: boolean) => {
                    const current = config.faking.tls_mod || [];
                    const next = checked
                      ? [...current.filter((m) => m !== "dupsid"), "dupsid"]
                      : current.filter((m) => m !== "dupsid");
                    onChange("faking.tls_mod", next);
                  }}
                  disabled={!config.faking.sni}
                />
              </Field>
            </FieldGroup>
          </FieldSet>
        )}

        <Separator className="my-4" />

        {/* SNI Mutation Section */}
        <FieldSet>
          <FieldLegend>ClientHello Mutation</FieldLegend>
          <FieldDescription>
            Modify TLS ClientHello structure to evade fingerprinting
          </FieldDescription>

          <Field>
            <FieldLabel>Mutation Mode</FieldLabel>
            <Select
              value={mutation.mode}
              onValueChange={(value) =>
                onChange("faking.sni_mutation.mode", value as string)
              }
            >
              <SelectTrigger>
                <SelectValue placeholder="Select mutation mode" />
              </SelectTrigger>
              <SelectContent>
                {MUTATION_MODES.map((option) => (
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
              {mutationModeDescriptions[mutation.mode]}
            </FieldDescription>
          </Field>
        </FieldSet>

        {isMutationEnabled && (
          <>
            <Separator className="my-4" />
            {showGreaseSettings && (
              <FieldSet>
                <FieldLegend>GREASE Configuration</FieldLegend>
                <Field>
                  <FieldLabel>
                    GREASE Extension Count
                    <Badge variant="secondary">{mutation.grease_count}</Badge>
                  </FieldLabel>
                  <Slider
                    value={[mutation.grease_count]}
                    onValueChange={([value]: [number]) =>
                      onChange("faking.sni_mutation.grease_count", value)
                    }
                    min={1}
                    max={10}
                    step={1}
                  />
                  <FieldDescription>
                    Number of GREASE extensions to insert
                  </FieldDescription>
                </Field>
              </FieldSet>
            )}

            {showPaddingSettings && (
              <>
                <Separator className="my-4" />
                <FieldSet>
                  <FieldLegend>Padding Configuration</FieldLegend>

                  <Field>
                    <FieldLabel>
                      Padding Size
                      <Badge variant="secondary">
                        {mutation.padding_size} bytes
                      </Badge>
                    </FieldLabel>
                    <Slider
                      value={[mutation.padding_size]}
                      onValueChange={([value]: [number]) =>
                        onChange("faking.sni_mutation.padding_size", value)
                      }
                      min={256}
                      max={16384}
                      step={256}
                    />
                    <FieldDescription>
                      Target ClientHello size with padding
                    </FieldDescription>
                  </Field>
                </FieldSet>
              </>
            )}

            {showFakeExtSettings && (
              <>
                <Separator className="my-4" />
                <FieldSet>
                  <FieldLegend>Fake Extensions Configuration</FieldLegend>
                  <Field>
                    <FieldLabel>
                      Fake Extension Count
                      <Badge variant="secondary">
                        {mutation.fake_ext_count}
                      </Badge>
                    </FieldLabel>
                    <Slider
                      value={[mutation.fake_ext_count]}
                      onValueChange={([value]: [number]) =>
                        onChange("faking.sni_mutation.fake_ext_count", value)
                      }
                      min={1}
                      max={15}
                      step={1}
                    />
                    <FieldDescription>
                      Number of fake TLS extensions to insert
                    </FieldDescription>
                  </Field>
                </FieldSet>
              </>
            )}

            {showFakeSniSettings && (
              <>
                <Separator className="my-4" />
                <FieldSet>
                  <FieldLegend>Fake SNI Configuration</FieldLegend>
                  <Field>
                    <FieldLabel>Add Fake SNI</FieldLabel>
                    <TagsInput
                      value={mutation.fake_snis || []}
                      onValueChange={(values) => {
                        const clean = Array.from(
                          new Set(
                            values
                              .map((v) => v.trim().toLowerCase())
                              .filter(Boolean),
                          ),
                        );
                        onChange("faking.sni_mutation.fake_snis", clean);
                      }}
                      placeholder="e.g., ya.ru, vk.com"
                    />
                    <FieldDescription>
                      Additional SNI values to inject into ClientHello
                    </FieldDescription>
                  </Field>
                </FieldSet>
              </>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
};

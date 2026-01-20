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
  FieldSeparator,
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

const FAKE_PAYLOAD_TYPES: Array<{ value: FakingPayloadType; label: string }> = [
  { value: FakingPayloadType.RANDOM, label: "Random" },
  { value: FakingPayloadType.CUSTOM, label: "Custom" },
  { value: FakingPayloadType.DEFAULT, label: "Preset: Google (classic)" },
  { value: FakingPayloadType.DEFAULT2, label: "Preset: DuckDuckGo" },
  { value: FakingPayloadType.CAPTURE, label: "Captured Payload" },
] as const;

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
        <FieldGroup>
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
          <div className="flex flex-row gap-4">
            <Field>
              <FieldLabel>Fake Strategy</FieldLabel>
              <Select
                value={config.faking.strategy}
                onValueChange={(value) =>
                  onChange("faking.strategy", value as FakingStrategy)
                }
                disabled={!config.faking.sni}
                items={FAKE_STRATEGIES}
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
                value={config.faking.sni_type}
                onValueChange={(value) =>
                  onChange("faking.sni_type", value as FakingPayloadType)
                }
                disabled={!config.faking.sni}
                items={FAKE_PAYLOAD_TYPES}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select payload type" />
                </SelectTrigger>
                <SelectContent>
                  {FAKE_PAYLOAD_TYPES.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>Content of fake packets</FieldDescription>
            </Field>
          </div>
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
                      <SelectValue placeholder="Select payload..." />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">Select payload...</SelectItem>
                      {captures.map((c) => (
                        <SelectItem key={c.filepath} value={c.filepath}>
                          {c.domain} ({c.size} bytes)
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {captures.length === 0
                      ? "No payloads available.Capture one in Settings."
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
                      Navigate to Settings to capture or upload TLS ClientHello
                      payloads.
                    </Link>
                  </AlertDescription>
                </Alert>
              </Field>
            </>
          )}

          <div className="flex flex-row gap-4">
            <Field>
              <FieldLabel>
                Fake TTL
                <Badge variant="secondary" className="font-semibold">
                  {config.faking.ttl}
                </Badge>
              </FieldLabel>
              <Slider
                value={[config.faking.ttl]}
                onValueChange={(val) => onChange("faking.ttl", val as number)}
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
              <FieldLabel>
                Fake Packet Count
                <Badge variant="secondary">
                  {config.faking.sni_seq_length}
                </Badge>
              </FieldLabel>
              <Slider
                value={[config.faking.sni_seq_length]}
                onValueChange={(val) =>
                  onChange("faking.sni_seq_length", val as number)
                }
                min={1}
                max={20}
                step={1}
                disabled={!config.faking.sni}
              />
              <FieldDescription>
                Number of fake packets to send
              </FieldDescription>
            </Field>
          </div>

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
        </FieldGroup>

        {/* TLS Mod Options - only show when payload has TLS structure */}
        {config.faking.sni_type !== FakingPayloadType.RANDOM && (
          <>
            <FieldSeparator className="my-6">
              Fake Packet TLS Modification
            </FieldSeparator>
            <FieldGroup>
              <Field>
                <Alert>
                  <AlertDescription className="text-center">
                    Modify fake TLS ClientHello to improve bypass (zapret-style)
                  </AlertDescription>
                </Alert>
              </Field>

              <div className="flex flex-row gap-4">
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
              </div>
            </FieldGroup>
          </>
        )}

        {/* SNI Mutation Section */}
        <FieldSeparator className="my-6">ClientHello Mutation</FieldSeparator>
        <FieldGroup>
          <Field>
            <Alert>
              <AlertDescription className="text-center">
                Modify TLS ClientHello structure to evade fingerprinting
              </AlertDescription>
            </Alert>
          </Field>

          <Field>
            <FieldLabel>Mutation Mode</FieldLabel>
            <Select
              value={mutation.mode}
              onValueChange={(value) =>
                onChange("faking.sni_mutation.mode", value as MutationMode)
              }
              items={MUTATION_MODES}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select mutation mode" />
              </SelectTrigger>
              <SelectContent>
                {MUTATION_MODES.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {mutationModeDescriptions[mutation.mode]}
            </FieldDescription>
          </Field>
        </FieldGroup>

        {showGreaseSettings && (
          <>
            <FieldSeparator className="my-6">
              GREASE Configuration
            </FieldSeparator>
            <FieldGroup>
              <Field>
                <FieldLabel>
                  GREASE Extension Count
                  <Badge variant="secondary" className="ml-auto">
                    {mutation.grease_count}
                  </Badge>
                </FieldLabel>
                <Slider
                  value={[mutation.grease_count]}
                  onValueChange={(val) =>
                    onChange("faking.sni_mutation.grease_count", val as number)
                  }
                  min={1}
                  max={10}
                  step={1}
                />
                <FieldDescription>
                  Number of GREASE extensions to insert
                </FieldDescription>
              </Field>
            </FieldGroup>
          </>
        )}

        {showPaddingSettings && (
          <>
            <FieldSeparator className="my-6">
              Padding Configuration
            </FieldSeparator>
            <FieldGroup>
              <Field>
                <FieldLabel>
                  Padding Size
                  <Badge variant="secondary" className="ml-auto">
                    {mutation.padding_size} bytes
                  </Badge>
                </FieldLabel>
                <Slider
                  value={[mutation.padding_size]}
                  onValueChange={(val) =>
                    onChange("faking.sni_mutation.padding_size", val as number)
                  }
                  min={256}
                  max={16384}
                  step={256}
                />
                <FieldDescription>
                  Target ClientHello size with padding
                </FieldDescription>
              </Field>
            </FieldGroup>
          </>
        )}

        {showFakeExtSettings && (
          <>
            <FieldSeparator className="my-6">
              Fake Extensions Configuration
            </FieldSeparator>
            <FieldGroup>
              <Field>
                <FieldLabel>
                  Fake Extension Count
                  <Badge variant="secondary" className="ml-auto">
                    {mutation.fake_ext_count}
                  </Badge>
                </FieldLabel>
                <Slider
                  value={[mutation.fake_ext_count]}
                  onValueChange={(val) =>
                    onChange(
                      "faking.sni_mutation.fake_ext_count",
                      val as number,
                    )
                  }
                  min={1}
                  max={15}
                  step={1}
                />
                <FieldDescription>
                  Number of fake TLS extensions to insert
                </FieldDescription>
              </Field>
            </FieldGroup>
          </>
        )}

        {showFakeSniSettings && (
          <>
            <FieldSeparator className="my-6">
              Fake SNI Configuration
            </FieldSeparator>
            <FieldGroup>
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
            </FieldGroup>
          </>
        )}
      </CardContent>
    </Card>
  );
};

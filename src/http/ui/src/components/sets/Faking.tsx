import { useCaptures } from "@b4.capture";
import { ClientHelloIcon, FakingIcon, AddIcon, SecurityIcon } from "@b4.icons";
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
import { Textarea } from "@design/components/ui/textarea";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { B4SetConfig, FakingPayloadType, MutationMode } from "@models/config";

interface FakingSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[]
  ) => void;
}

const FAKE_STRATEGIES = [
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
  const [newFakeSni, setNewFakeSni] = useState("");
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

  const handleAddFakeSni = () => {
    if (newFakeSni.trim()) {
      const current = mutation.fake_snis || [];
      if (!current.includes(newFakeSni.trim())) {
        onChange("faking.sni_mutation.fake_snis", [
          ...current,
          newFakeSni.trim(),
        ]);
      }
      setNewFakeSni("");
    }
  };

  const handleRemoveFakeSni = (sni: string) => {
    const current = mutation.fake_snis || [];
    onChange(
      "faking.sni_mutation.fake_snis",
      current.filter((s) => s !== sni)
    );
  };

  return (
    <>
      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <FakingIcon />
            </div>
            <div className="flex-1">
              <CardTitle>Fake SNI Configuration</CardTitle>
              <CardDescription className="mt-1">
                Configure fake SNI packets to confuse DPI
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="md:col-span-2">
              <label htmlFor="switch-faking-sni">
                <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
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
              </label>
            </div>
            <div>
              <Field>
                <FieldLabel>Fake Strategy</FieldLabel>
                <Select
                  value={config.faking.strategy}
                  onValueChange={(value) =>
                    onChange("faking.strategy", value as string)
                  }
                  disabled={!config.faking.sni}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select fake strategy" />
                  </SelectTrigger>
                  <SelectContent>
                    {FAKE_STRATEGIES.map((option) => (
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
                  How to make fake packets unprocessable by server
                </FieldDescription>
              </Field>
            </div>
            <div>
              <div className="flex flex-col gap-4">
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
                  <div className="mt-4">
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
                        Hex-encoded payload for fake packets (use Capture
                        feature to get real payloads)
                      </FieldDescription>
                    </Field>
                  </div>
                )}
              </div>
            </div>
            {config.faking.sni_type === FakingPayloadType.CAPTURE && (
              <div className="md:col-span-2 grid grid-cols-1 md:grid-cols-2 gap-4">
                {captures.length > 0 && (
                  <div>
                    <Field>
                      <FieldLabel>Captured Payload</FieldLabel>
                      <Select
                        value={config.faking.payload_file || ""}
                        onValueChange={(value) =>
                          onChange("faking.payload_file", value as string)
                        }
                      >
                        <SelectTrigger>
                          <SelectValue placeholder="Select a capture..." />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="">Select a capture...</SelectItem>
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
                  </div>
                )}
                <div className={captures.length > 0 ? "" : "md:col-span-2"}>
                  <Alert>
                    <AlertDescription>
                      {captures.length === 0 &&
                        "No TLS captures available. You can use the Capture feature to record ClientHello payloads or  upload your own capture files."}

                      <Link to="/settings/capture">
                        {" "}
                        Navigate to the Settings section to capture or upload
                        your own TLS ClientHello payloads.
                      </Link>
                    </AlertDescription>
                  </Alert>
                </div>
              </div>
            )}
            <div>
              <Field className="w-full space-y-2">
                <div className="flex items-center justify-between">
                  <FieldLabel className="text-sm font-medium">
                    Fake TTL
                  </FieldLabel>
                  <Badge variant="secondary" className="font-semibold">
                    {config.faking.ttl}
                  </Badge>
                </div>
                <Slider
                  value={[config.faking.ttl]}
                  onValueChange={(values) => onChange("faking.ttl", values[0])}
                  min={1}
                  max={64}
                  step={1}
                  disabled={!config.faking.sni}
                  className="w-full"
                />
                <FieldDescription>
                  TTL for fake packets (should expire before server)
                </FieldDescription>
              </Field>
            </div>
            <div>
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
            </div>
            <div>
              <Field className="w-full space-y-2">
                <div className="flex items-center justify-between">
                  <FieldLabel className="text-sm font-medium">
                    Fake Packet Count
                  </FieldLabel>
                  <Badge variant="secondary" className="font-semibold">
                    {config.faking.sni_seq_length}
                  </Badge>
                </div>
                <Slider
                  value={[config.faking.sni_seq_length]}
                  onValueChange={(values) =>
                    onChange("faking.sni_seq_length", values[0])
                  }
                  min={1}
                  max={20}
                  step={1}
                  disabled={!config.faking.sni}
                  className="w-full"
                />
                <FieldDescription>
                  Number of fake packets to send
                </FieldDescription>
              </Field>
            </div>
            {/* TLS Mod Options - only show when payload has TLS structure */}
            {config.faking.sni_type !== FakingPayloadType.RANDOM && (
              <div className="md:col-span-2">
                <p className="text-sm font-semibold mb-2">
                  Fake Packet TLS Modification
                </p>
                <p className="text-xs text-muted-foreground mb-4">
                  Modify fake TLS ClientHello to improve bypass (zapret-style)
                </p>
                <div className="flex flex-row gap-4">
                  <label htmlFor="switch-faking-tls-rnd">
                    <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
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
                  </label>
                  <label htmlFor="switch-faking-tls-dupsid">
                    <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
                      <FieldContent>
                        <FieldTitle>Duplicate Session ID</FieldTitle>
                        <FieldDescription>
                          Copy Session ID from real ClientHello into fake
                        </FieldDescription>
                      </FieldContent>
                      <Switch
                        id="switch-faking-tls-dupsid"
                        checked={(config.faking.tls_mod || []).includes(
                          "dupsid"
                        )}
                        onCheckedChange={(checked: boolean) => {
                          const current = config.faking.tls_mod || [];
                          const next = checked
                            ? [
                                ...current.filter((m) => m !== "dupsid"),
                                "dupsid",
                              ]
                            : current.filter((m) => m !== "dupsid");
                          onChange("faking.tls_mod", next);
                        }}
                        disabled={!config.faking.sni}
                      />
                    </Field>
                  </label>
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* SNI Mutation Section */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <ClientHelloIcon className="rotate-45" />
            </div>
            <div className="flex-1">
              <CardTitle>ClientHello Mutation</CardTitle>
              <CardDescription className="mt-1">
                Modify TLS ClientHello structure to evade fingerprinting
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
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
            </div>

            {isMutationEnabled && (
              <>
                {showGreaseSettings && (
                  <>
                    <div className="relative my-4 md:col-span-2 flex items-center">
                      <Separator className="absolute inset-0 top-1/2" />
                      <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                        GREASE Configuration
                      </span>
                    </div>
                    <div className="md:col-span-2">
                      <Field className="w-full space-y-2">
                        <div className="flex items-center justify-between">
                          <FieldLabel className="text-sm font-medium">
                            GREASE Extension Count
                          </FieldLabel>
                          <Badge variant="secondary" className="font-semibold">
                            {mutation.grease_count}
                          </Badge>
                        </div>
                        <Slider
                          value={[mutation.grease_count]}
                          onValueChange={(values) =>
                            onChange(
                              "faking.sni_mutation.grease_count",
                              values[0]
                            )
                          }
                          min={1}
                          max={10}
                          step={1}
                          className="w-full"
                        />
                        <FieldDescription>
                          Number of GREASE extensions to insert
                        </FieldDescription>
                      </Field>
                    </div>
                  </>
                )}

                {showPaddingSettings && (
                  <>
                    <div className="relative my-4 md:col-span-2 flex items-center">
                      <Separator className="absolute inset-0 top-1/2" />
                      <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                        Padding Configuration
                      </span>
                    </div>
                    <div className="md:col-span-2">
                      <Field className="w-full space-y-2">
                        <div className="flex items-center justify-between">
                          <FieldLabel className="text-sm font-medium">
                            Padding Size
                          </FieldLabel>
                          <Badge variant="secondary" className="font-semibold">
                            {mutation.padding_size} bytes
                          </Badge>
                        </div>
                        <Slider
                          value={[mutation.padding_size]}
                          onValueChange={(values) =>
                            onChange(
                              "faking.sni_mutation.padding_size",
                              values[0]
                            )
                          }
                          min={256}
                          max={16384}
                          step={256}
                          className="w-full"
                        />
                        <FieldDescription>
                          Target ClientHello size with padding
                        </FieldDescription>
                      </Field>
                    </div>
                  </>
                )}

                {showFakeExtSettings && (
                  <>
                    <div className="relative my-4 md:col-span-2 flex items-center">
                      <Separator className="absolute inset-0 top-1/2" />
                      <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                        Fake Extensions Configuration
                      </span>
                    </div>
                    <div className="md:col-span-2">
                      <Field className="w-full space-y-2">
                        <div className="flex items-center justify-between">
                          <FieldLabel className="text-sm font-medium">
                            Fake Extension Count
                          </FieldLabel>
                          <Badge variant="secondary" className="font-semibold">
                            {mutation.fake_ext_count}
                          </Badge>
                        </div>
                        <Slider
                          value={[mutation.fake_ext_count]}
                          onValueChange={(values) =>
                            onChange(
                              "faking.sni_mutation.fake_ext_count",
                              values[0]
                            )
                          }
                          min={1}
                          max={15}
                          step={1}
                          className="w-full"
                        />
                        <FieldDescription>
                          Number of fake TLS extensions to insert
                        </FieldDescription>
                      </Field>
                    </div>
                  </>
                )}

                {showFakeSniSettings && (
                  <>
                    <div className="relative my-4 md:col-span-2 flex items-center">
                      <Separator className="absolute inset-0 top-1/2" />
                      <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                        Fake SNI Configuration
                      </span>
                    </div>
                    <div>
                      <div className="flex gap-2 items-start">
                        <Field className="flex-1">
                          <FieldLabel>Add Fake SNI</FieldLabel>
                          <Input
                            value={newFakeSni}
                            onChange={(e) => setNewFakeSni(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                handleAddFakeSni();
                              }
                            }}
                            placeholder="e.g., ya.ru, vk.com"
                          />
                          <FieldDescription>
                            Additional SNI values to inject into ClientHello
                          </FieldDescription>
                        </Field>
                        <Button
                          variant="secondary"
                          size="icon"
                          onClick={handleAddFakeSni}
                          disabled={!newFakeSni.trim()}
                        >
                          <AddIcon className="h-4 w-4" />
                        </Button>
                      </div>
                    </div>
                    <div>
                      <ChipList
                        items={mutation.fake_snis || []}
                        getKey={(s) => s}
                        getLabel={(s) => s}
                        onDelete={handleRemoveFakeSni}
                        title="Active Fake SNIs"
                      />
                    </div>
                  </>
                )}
              </>
            )}
          </div>
        </CardContent>
      </Card>
    </>
  );
};

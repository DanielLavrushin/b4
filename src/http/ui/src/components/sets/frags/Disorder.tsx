import { TagsInput } from "@composed/tags-input";
import { Alert, AlertDescription } from "@design/primitives/alert";
import { B4SetConfig, DisorderShuffleMode } from "@models/config";
import { Badge } from "@primitives/badge";
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
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";
import { useState } from "react";

const SEQ_OVERLAP_PRESETS = [
  { label: "None", value: "none", pattern: [] },
  {
    label: "TLS 1.2 Header",
    value: "tls12",
    pattern: ["16", "03", "03", "00", "00"],
  },
  {
    label: "TLS 1.1 Header",
    value: "tls11",
    pattern: ["16", "03", "02", "00", "00"],
  },
  {
    label: "TLS 1.0 Header",
    value: "tls10",
    pattern: ["16", "03", "01", "00", "00"],
  },
  {
    label: "HTTP GET",
    value: "http_get",
    pattern: ["47", "45", "54", "20", "2F"],
  },
  { label: "Zeros", value: "zeros", pattern: ["00"] },
  { label: "Custom", value: "custom", pattern: [] },
];

interface DisorderSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

const shuffleModeOptions: { label: string; value: DisorderShuffleMode }[] = [
  { label: "Full Shuffle", value: "full" },
  { label: "Reverse Order", value: "reverse" },
];

export const DisorderSettings = ({
  config,
  onChange,
}: DisorderSettingsProps) => {
  const disorder = config.fragmentation.disorder;
  const middleSni = config.fragmentation.middle_sni;

  const [customMode, setCustomMode] = useState(false);
  const seqPattern = config.fragmentation.seq_overlap_pattern || [];

  const getCurrentPreset = () => {
    if (customMode) return "custom";
    if (seqPattern.length === 0) return "none";
    if (seqPattern.length === 0) return "custom";

    const match = SEQ_OVERLAP_PRESETS.find(
      (p) =>
        p.value !== "none" &&
        p.value !== "custom" &&
        p.pattern.length === seqPattern.length &&
        p.pattern.every((b, i) => b === seqPattern[i]),
    );
    return match?.value || "custom";
  };

  const handlePresetChange = (preset: string) => {
    if (preset === "none") {
      setCustomMode(false);
      onChange("fragmentation.seq_overlap_pattern", []);
      return;
    }

    if (preset === "custom") {
      onChange("fragmentation.seq_overlap_pattern", []);
      setCustomMode(true);

      return;
    }

    setCustomMode(false);
    const found = SEQ_OVERLAP_PRESETS.find((p) => p.value === preset);
    if (found) {
      onChange("fragmentation.seq_overlap_pattern", found.pattern);
    }
  };

  return (
    <>
      <FieldSeparator className="my-6">Disorder Strategy</FieldSeparator>
      <FieldGroup>
        <Field>
          <Alert>
            <AlertDescription className="text-center">
              Disorder sends real TCP segments out of order with timing jitter.
              No fake packets — exploits DPI that expects sequential data.
            </AlertDescription>
          </Alert>
        </Field>

        {/* SNI Split Toggle */}
        <div className="flex flex-row gap-4">
          <Field>
            <FieldLabel>Shuffle Mode</FieldLabel>
            <Select
              value={disorder.shuffle_mode}
              onValueChange={(value) =>
                onChange("fragmentation.disorder.shuffle_mode", value as string)
              }
              items={shuffleModeOptions}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select shuffle mode" />
              </SelectTrigger>
              <SelectContent>
                {shuffleModeOptions.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value.toString()}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>How to reorder segments</FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>SNI-Based Splitting</FieldTitle>
              <FieldDescription>
                Split around SNI hostname for targeted disruption
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-disorder-sni"
              checked={middleSni}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.middle_sni", checked)
              }
            />
          </Field>
        </div>
      </FieldGroup>

      <FieldSeparator className="my-6">Timing Jitter</FieldSeparator>

      <FieldGroup>
        <Field>
          <Alert>
            <AlertDescription className="text-center">
              Random delay between segments. Used when TCP Seg2Delay is 0.
            </AlertDescription>
          </Alert>
        </Field>

        <Field>
          <FieldLabel>
            Jitter Range
            <Badge variant="secondary" className="ml-auto">
              {disorder.min_jitter_us} - {disorder.max_jitter_us} μs
            </Badge>
          </FieldLabel>
          <Slider
            value={[disorder.min_jitter_us, disorder.max_jitter_us]}
            onValueChange={(values) => {
              const [min, max] = values as number[];
              onChange("fragmentation.disorder.min_jitter_us", min);
              onChange("fragmentation.disorder.max_jitter_us", max);
            }}
            min={100}
            max={10000}
            step={100}
          />
          <FieldDescription>
            Minimum and maximum delay between segments (μs)
          </FieldDescription>
        </Field>
      </FieldGroup>
      <FieldSeparator className="my-6">
        Sequence Overlap (seqovl)
      </FieldSeparator>

      <FieldGroup>
        <Field>
          <Alert>
            <AlertDescription className="text-center">
              Prepends fake bytes with decreased TCP sequence number. DPI sees
              fake protocol header, server discards overlap.
            </AlertDescription>
          </Alert>
        </Field>

        <Field>
          <FieldLabel>Overlap Pattern</FieldLabel>
          <Select
            value={getCurrentPreset()}
            onValueChange={(value) => handlePresetChange(value as string)}
            items={SEQ_OVERLAP_PRESETS}
          >
            <SelectTrigger>
              <SelectValue placeholder="Select overlap pattern" />
            </SelectTrigger>
            <SelectContent>
              {SEQ_OVERLAP_PRESETS.map((p) => (
                <SelectItem key={p.value} value={p.value}>
                  {p.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription>Fake bytes DPI will see</FieldDescription>
        </Field>

        {getCurrentPreset() === "custom" && (
          <Field>
            <FieldLabel>Byte Pattern (hex)</FieldLabel>
            <TagsInput
              value={seqPattern.map((b) => `0x${b}`)}
              onValueChange={(values) => {
                const bytesSet = new Set<string>();
                values.forEach((v) => {
                  const byte = v.trim().replace(/^0x/i, "").toUpperCase();
                  if (/^[0-9A-F]{1,2}$/.test(byte)) {
                    bytesSet.add(byte.padStart(2, "0"));
                  }
                });
                onChange(
                  "fragmentation.seq_overlap_pattern",
                  Array.from(bytesSet),
                );
              }}
              placeholder="e.g., 16 or 0x16"
            />
            <FieldDescription>
              Enter hex bytes (00-FF) for custom pattern
            </FieldDescription>
          </Field>
        )}

        {seqPattern.length > 0 && (
          <Field>
            <div className="md:col-span-2">
              <div className="bg-card border-border border p-4">
                <p className="text-muted-foreground mb-2 text-xs">
                  SEQOVL Visualization
                </p>

                <div className="flex items-center gap-2">
                  <div className="border-secondary border-2 border-dashed p-2">
                    [{seqPattern.join(" ")}] (fake, seq-{seqPattern.length})
                  </div>
                  <p className="mx-2">+</p>
                  <div className="flex-1 p-2">Real TLS ClientHello...</div>
                </div>
                <p className="text-muted-foreground mt-2 text-xs">
                  DPI sees fake header at decreased seq#, server reassembles
                  correctly
                </p>
              </div>
            </div>
          </Field>
        )}
      </FieldGroup>
    </>
  );
};

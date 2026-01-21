import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
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
import { TagsInput } from "@composed/tags-input";
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
import { B4SetConfig, DisorderShuffleMode } from "@models/config";
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
      <FieldSet>
        <FieldLegend>Disorder Strategy</FieldLegend>
        <FieldDescription>
          Disorder sends real TCP segments out of order with timing jitter. No
          fake packets — exploits DPI that expects sequential data.
        </FieldDescription>

        {/* SNI Split Toggle */}
        <FieldGroup>
          <Field>
            <FieldLabel>Shuffle Mode</FieldLabel>
            <Select
              value={disorder.shuffle_mode}
              onValueChange={(value) =>
                onChange("fragmentation.disorder.shuffle_mode", value as string)
              }
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
        </FieldGroup>
      </FieldSet>

      <Separator className="my-4" />

      {/* Visual */}
      <FieldSet>
        <FieldLegend>Segment Order Example</FieldLegend>
        <FieldDescription>
          {disorder.shuffle_mode === "full"
            ? "Segments sent in random order"
            : "Segments sent in reverse order"}
        </FieldDescription>
        <div className="flex items-center gap-2">
          <div className="flex gap-1 font-mono">
            {["1", "2", "3", "4"].map((n, i) => (
              <div
                key={i}
                className="bg-accent min-w-8 rounded p-2 text-center"
              >
                {n}
              </div>
            ))}
          </div>
          <p className="mx-2">→</p>
          <div className="flex gap-1 font-mono">
            {(disorder.shuffle_mode === "reverse"
              ? ["4", "3", "2", "1"]
              : ["3", "1", "4", "2"]
            ).map((n, i) => (
              <div key={i} className="bg-tertiary min-w-8 p-2 text-center">
                {n}
              </div>
            ))}
          </div>
        </div>
      </FieldSet>

      <Separator className="my-4" />

      <FieldSet>
        <FieldLegend>Timing Jitter</FieldLegend>
        <FieldDescription>
          Random delay between segments. Used when TCP Seg2Delay is 0.
        </FieldDescription>
        <Field>
          <FieldLabel>
            Jitter Range
            <Badge variant="secondary">
              {disorder.min_jitter_us} - {disorder.max_jitter_us} μs
            </Badge>
          </FieldLabel>
          <Slider
            value={[disorder.min_jitter_us, disorder.max_jitter_us]}
            onValueChange={(values) => {
              const [min, max] = values;
              onChange("fragmentation.disorder.min_jitter_us", min);
              onChange("fragmentation.disorder.max_jitter_us", max);
            }}
            min={100}
            max={10000}
            step={100}
            className="w-full"
          />
          <FieldDescription>
            Minimum and maximum delay between segments (μs)
          </FieldDescription>
        </Field>
      </FieldSet>

      <Separator className="my-4" />

      <FieldSet>
        <FieldLegend>Sequence Overlap (seqovl)</FieldLegend>
        <FieldDescription>
          Prepends fake bytes with decreased TCP sequence number. DPI sees fake
          protocol header, server discards overlap.
        </FieldDescription>

        <Field>
          <FieldLabel>Overlap Pattern</FieldLabel>
          <Select
            value={getCurrentPreset()}
            onValueChange={(value) => handlePresetChange(value as string)}
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
      </FieldSet>

      {seqPattern.length > 0 && (
        <>
          <Separator className="my-4" />

          <FieldSet>
            <FieldLegend>SEQOVL Visualization</FieldLegend>
            <FieldDescription>
              DPI sees fake header at decreased seq#, server reassembles
              correctly
            </FieldDescription>
            <div className="flex items-center gap-2">
              <div className="border-secondary border-2 border-dashed p-2">
                [{seqPattern.join(" ")}] (fake, seq-{seqPattern.length})
              </div>
              <p className="mx-2">+</p>
              <div className="flex-1 p-2">Real TLS ClientHello...</div>
            </div>
          </FieldSet>
        </>
      )}
    </>
  );
};

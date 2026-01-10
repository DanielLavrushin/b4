import { AddIcon } from "@b4.icons";
import { ChipList } from "@components/common/ChipList";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
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
    value: string | boolean | number | string[]
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
  const [newByte, setNewByte] = useState("");
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
        p.pattern.every((b, i) => b === seqPattern[i])
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

  const handleAddByte = () => {
    const bytes = [] as string[];
    newByte.split(" ").forEach((b) => {
      const byte = b.trim().replace(/^0x/i, "").toUpperCase();
      if (/^[0-9A-F]{1,2}$/.test(byte)) {
        const padded = byte.padStart(2, "0");
        bytes.push(padded);
      }
    });
    onChange("fragmentation.seq_overlap_pattern", [...seqPattern, ...bytes]);
    setNewByte("");
  };

  const handleRemoveByte = (index: number) => {
    onChange(
      "fragmentation.seq_overlap_pattern",
      seqPattern.filter((_, i) => i !== index)
    );
  };

  return (
    <>
      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
          Disorder Strategy
        </span>
      </div>
      <Alert className="m-0">
        <AlertDescription>
          Disorder sends real TCP segments out of order with timing jitter. No
          fake packets — exploits DPI that expects sequential data.
        </AlertDescription>
      </Alert>

      {/* SNI Split Toggle */}
      <div>
        <label htmlFor="switch-disorder-sni">
          <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
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
        </label>
      </div>

      <div>
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
                <SelectItem key={option.value} value={option.value.toString()}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FieldDescription>How to reorder segments</FieldDescription>
        </Field>
      </div>

      {/* Visual */}
      <div className="md:col-span-2">
        <div className="p-4 bg-card rounded-md border border-border">
          <p className="text-xs text-muted-foreground mb-2">
            SEGMENT ORDER EXAMPLE
          </p>
          <div className="flex gap-2 items-center">
            <div className="flex gap-1 font-mono">
              {["①", "②", "③", "④"].map((n, i) => (
                <div
                  key={i}
                  className="p-2 bg-accent rounded min-w-8 text-center"
                >
                  {n}
                </div>
              ))}
            </div>
            <p className="mx-2">→</p>
            <div className="flex gap-1 font-mono">
              {(disorder.shuffle_mode === "reverse"
                ? ["④", "③", "②", "①"]
                : ["③", "①", "④", "②"]
              ).map((n, i) => (
                <div
                  key={i}
                  className="p-2 bg-tertiary rounded min-w-8 text-center"
                >
                  {n}
                </div>
              ))}
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            {disorder.shuffle_mode === "full"
              ? "Segments sent in random order (example shown)"
              : "Segments sent in reverse order"}
          </p>
        </div>
      </div>

      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
          Timing Jitter
        </span>
      </div>
      <Alert className="m-0 md:col-span-2">
        <AlertDescription>
          Random delay between segments. Used when TCP Seg2Delay is 0.
        </AlertDescription>
      </Alert>

      <div>
        <Field className="w-full space-y-2">
          <div className="flex items-center justify-between">
            <FieldLabel className="text-sm font-medium">Min Jitter</FieldLabel>
            <Badge variant="secondary" className="font-semibold">
              {disorder.min_jitter_us} μs
            </Badge>
          </div>
          <Slider
            value={[disorder.min_jitter_us]}
            onValueChange={(values) =>
              onChange("fragmentation.disorder.min_jitter_us", values[0])
            }
            min={100}
            max={5000}
            step={100}
            className="w-full"
          />
          <FieldDescription>
            Minimum delay between segments (μs)
          </FieldDescription>
        </Field>
      </div>

      <div>
        <Field className="w-full space-y-2">
          <div className="flex items-center justify-between">
            <FieldLabel className="text-sm font-medium">Max Jitter</FieldLabel>
            <Badge variant="secondary" className="font-semibold">
              {disorder.max_jitter_us} μs
            </Badge>
          </div>
          <Slider
            value={[disorder.max_jitter_us]}
            onValueChange={(values) =>
              onChange("fragmentation.disorder.max_jitter_us", values[0])
            }
            min={500}
            max={10000}
            step={100}
            className="w-full"
          />
          <FieldDescription>
            Maximum delay between segments (μs)
          </FieldDescription>
        </Field>
      </div>

      {disorder.min_jitter_us >= disorder.max_jitter_us && (
        <Alert variant="destructive">
          <AlertDescription>
            Max jitter should be greater than min jitter for random variation.
          </AlertDescription>
        </Alert>
      )}

      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
          Sequence Overlap (seqovl)
        </span>
      </div>

      <Alert className="m-0 md:col-span-2">
        <AlertDescription>
          Prepends fake bytes with decreased TCP sequence number. DPI sees fake
          protocol header, server discards overlap.
        </AlertDescription>
      </Alert>

      <div>
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
      </div>
      {seqPattern.length > 0 && (
        <div>
          <div className="p-4 bg-card rounded-md border border-border">
            <p className="text-xs text-muted-foreground mb-2">
              SEQOVL VISUALIZATION
            </p>
            <div className="flex gap-1 font-mono text-xs items-center">
              <div className="p-2 bg-tertiary rounded border-2 border-dashed border-secondary">
                [{seqPattern.join(" ")}] (fake, seq-
                {seqPattern.length})
              </div>
              <p className="mx-1">+</p>
              <div className="p-2 bg-accent-secondary rounded flex-1">
                Real TLS ClientHello...
              </div>
            </div>
            <p className="text-xs text-muted-foreground mt-2">
              DPI sees fake header at decreased seq#, server reassembles
              correctly
            </p>
          </div>
        </div>
      )}
      {getCurrentPreset() === "custom" && (
        <>
          <div>
            <div className="flex gap-2">
              <Field className="flex-1">
                <FieldLabel>Add Byte (hex)</FieldLabel>
                <Input
                  value={newByte}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setNewByte(e.target.value)
                  }
                  onKeyDown={(e: React.KeyboardEvent<HTMLInputElement>) =>
                    e.key === "Enter" && (e.preventDefault(), handleAddByte())
                  }
                  placeholder="e.g., 16 or 0x16"
                />
              </Field>
              <Button
                variant="secondary"
                size="icon"
                onClick={handleAddByte}
                disabled={!newByte.trim()}
              >
                <AddIcon className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div>
            <ChipList
              items={seqPattern.map((b, i) => ({ byte: b, index: i }))}
              getKey={(item) => `${item.byte}-${item.index}`}
              getLabel={(item) => `0x${item.byte}`}
              onDelete={(item) => handleRemoveByte(item.index)}
              emptyMessage="Add hex bytes for custom pattern"
              showEmpty
            />
          </div>
        </>
      )}
    </>
  );
};

import { TagsInput } from "@composed/tags-input";
import { B4SetConfig, ComboShuffleMode } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Checkbox } from "@primitives/checkbox";
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

interface ComboSettingsProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

const shuffleModeOptions: { label: string; value: ComboShuffleMode }[] = [
  { label: "Middle Only", value: "middle" },
  { label: "Full Shuffle", value: "full" },
  { label: "Reverse Order", value: "reverse" },
];

export const ComboSettings = ({ config, onChange }: ComboSettingsProps) => {
  const combo = config.fragmentation.combo;
  const middleSni = config.fragmentation.middle_sni;
  const decoySNIs = combo.decoy_snis || [];

  const enabledSplits = [
    combo.first_byte_split && "1st byte",
    combo.extension_split && "ext",
    middleSni && "SNI",
  ].filter(Boolean);

  return (
    <>
      <FieldSet>
        <FieldLegend>Combo Strategy</FieldLegend>

        <FieldDescription>
          Combo combines multiple split points and sends segments out of order
          with timing jitter to confuse stateful DPI.
        </FieldDescription>

        {/* Decoy Settings */}

        <Field orientation="horizontal">
          <FieldContent>
            <FieldTitle>Enable Decoy</FieldTitle>
            <FieldDescription>
              Send fake ClientHello with whitelisted SNI before real traffic
            </FieldDescription>
          </FieldContent>
          <Switch
            id="switch-combo-decoy"
            checked={combo.decoy_enabled}
            onCheckedChange={(checked: boolean) =>
              onChange("fragmentation.combo.decoy_enabled", checked)
            }
          />
        </Field>

        {combo.decoy_enabled && (
          <>
            <Field>
              <FieldLabel>Decoy SNI Domains</FieldLabel>
              <TagsInput
                value={decoySNIs}
                onValueChange={(values) => {
                  onChange(
                    "fragmentation.combo.decoy_snis",
                    values.map((v) => v.trim().toLowerCase()),
                  );
                }}
                placeholder="e.g. ya.ru, vk.com, mail.ru, dzen.ru"
              />
              <FieldDescription>
                Whitelisted domains used in decoy packet. DPI sees these instead
                of real target.
              </FieldDescription>
            </Field>

            {/* How Decoy Works Visualization */}

            <div className="bg-card border-border rounded-md border p-4">
              <p className="text-muted-foreground mb-3 text-xs font-semibold uppercase">
                HOW DECOY WORKS
              </p>
              <div className="flex flex-col gap-3">
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground min-w-20 text-xs">
                    Sent 1st:
                  </span>
                  <code className="bg-tertiary border-secondary rounded border-2 border-dashed px-2 py-1 font-mono text-xs">
                    {decoySNIs[0] || "ya.ru"} (DECOY, low TTL)
                  </code>
                  <span className="text-secondary ml-2 text-xs">
                    → DPI sees, dies before server
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground min-w-20 text-xs">
                    Sent 2nd:
                  </span>
                  <code className="bg-accent-secondary border-secondary rounded border-2 border-solid px-2 py-1 font-mono text-xs">
                    REAL (fragmented)
                  </code>
                  <span className="text-secondary ml-2 text-xs">
                    → Server receives
                  </span>
                </div>
              </div>
            </div>
          </>
        )}
      </FieldSet>

      <Separator className="my-4" />

      {/* Split Points */}
      <FieldSet>
        <FieldLegend>Split Points</FieldLegend>
        <FieldDescription>Where to split the packet</FieldDescription>

        <Field orientation="horizontal">
          <Checkbox
            id="checkbox-combo-first-byte"
            checked={combo.first_byte_split}
            onCheckedChange={(checked: boolean) =>
              onChange("fragmentation.combo.first_byte_split", checked)
            }
          />
          <FieldLabel>First Byte</FieldLabel>
        </Field>

        <Field orientation="horizontal">
          <Checkbox
            id="checkbox-combo-extension"
            checked={combo.extension_split}
            onCheckedChange={(checked: boolean) =>
              onChange("fragmentation.combo.extension_split", checked)
            }
          />
          <FieldLabel>Extension Split</FieldLabel>
        </Field>

        <Field orientation="horizontal">
          <Checkbox
            id="checkbox-combo-sni"
            checked={middleSni}
            onCheckedChange={(checked: boolean) =>
              onChange("fragmentation.middle_sni", checked)
            }
          />
          <FieldLabel>SNI Split</FieldLabel>
        </Field>

        {/* Visual */}
        <div className="md:col-span-2">
          <div className="bg-card border-border rounded-md border p-4">
            <p className="text-muted-foreground mb-2 text-xs">
              SEGMENT VISUALIZATION
            </p>
            <div className="flex flex-wrap gap-1 font-mono text-xs">
              {combo.first_byte_split && (
                <div className="bg-tertiary min-w-10 rounded p-2 text-center">
                  1B
                </div>
              )}
              {combo.extension_split && (
                <div className="bg-accent min-w-15 flex-1 rounded p-2 text-center">
                  Pre-SNI Ext
                </div>
              )}
              {middleSni && (
                <>
                  <div className="bg-accent-secondary min-w-12.5 rounded p-2 text-center">
                    SNI₁
                  </div>
                  <div className="bg-accent-secondary min-w-12.5 rounded p-2 text-center">
                    SNI₂
                  </div>
                </>
              )}
              <div className="bg-quaternary min-w-15 flex-1 rounded p-2 text-center">
                Rest...
              </div>
            </div>
            <p className="text-muted-foreground mt-2 text-xs">
              {enabledSplits.length > 0
                ? `Active splits: ${enabledSplits.join(" → ")} → creates ${
                    enabledSplits.length + 1
                  } segments`
                : "No splits enabled - packet sent as single segment"}
            </p>
          </div>
        </div>
      </FieldSet>

      <Separator className="my-4" />

      {/* Shuffle Mode */}

      <FieldSet>
        <FieldLegend>Shuffle</FieldLegend>
        <FieldDescription>
          How to reorder segments before sending
        </FieldDescription>
        <Field>
          <FieldLabel>Shuffle Mode</FieldLabel>
          <Select
            value={combo.shuffle_mode}
            onValueChange={(value) =>
              onChange("fragmentation.combo.shuffle_mode", value as string)
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
          <FieldDescription>
            {combo.shuffle_mode === "middle" &&
              "Middle: Keep first & last in place, shuffle middle segments"}
            {combo.shuffle_mode === "full" &&
              "Full: Randomly shuffle all segments"}
            {combo.shuffle_mode === "reverse" &&
              "Reverse: Send segments in reverse order"}
          </FieldDescription>
        </Field>
      </FieldSet>

      <Separator className="my-4" />

      <FieldSet>
        <FieldLegend>Timing Settings</FieldLegend>
        <FieldDescription>Delay between segments</FieldDescription>

        <FieldGroup>
          <Field>
            <FieldLabel>
              First Segment Delay
              <Badge variant="secondary">{combo.first_delay_ms} ms</Badge>
            </FieldLabel>
            <Slider
              value={[combo.first_delay_ms]}
              onValueChange={([value]: [number]) =>
                onChange("fragmentation.combo.first_delay_ms", value)
              }
              min={10}
              max={500}
              step={10}
            />
            <FieldDescription>Delay after first segment (ms)</FieldDescription>
          </Field>

          <Field>
            <FieldLabel>
              Jitter Max
              <Badge variant="secondary" className="font-semibold">
                {combo.jitter_max_us} μs
              </Badge>
            </FieldLabel>
            <Slider
              value={[combo.jitter_max_us]}
              onValueChange={([value]: [number]) =>
                onChange("fragmentation.combo.jitter_max_us", value)
              }
              min={100}
              max={10000}
              step={100}
            />
            <FieldDescription>
              Max random delay between other segments (μs)
            </FieldDescription>
          </Field>
        </FieldGroup>
      </FieldSet>

      {!combo.first_byte_split && !combo.extension_split && !middleSni && (
        <Alert variant="destructive" className="md:col-span-2">
          <AlertDescription>
            No split points enabled. Enable at least one for Combo to work
            effectively.
          </AlertDescription>
        </Alert>
      )}
    </>
  );
};

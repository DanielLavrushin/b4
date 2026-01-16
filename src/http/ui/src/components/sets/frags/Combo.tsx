import { useState } from "react";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
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
import { B4SetConfig, ComboShuffleMode } from "@models/config";
import { ChipList } from "@components/common/ChipList";
import { AddIcon } from "@b4.icons";

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
  const [newDomain, setNewDomain] = useState("");
  const combo = config.fragmentation.combo;
  const middleSni = config.fragmentation.middle_sni;
  const decoySNIs = combo.decoy_snis || [];

  const enabledSplits = [
    combo.first_byte_split && "1st byte",
    combo.extension_split && "ext",
    middleSni && "SNI",
  ].filter(Boolean);

  const handleAddDomain = () => {
    const domain = newDomain.trim().toLowerCase();
    if (domain && !decoySNIs.includes(domain)) {
      onChange("fragmentation.combo.decoy_snis", [...decoySNIs, domain]);
      setNewDomain("");
    }
  };

  const handleRemoveDomain = (domain: string) => {
    onChange(
      "fragmentation.combo.decoy_snis",
      decoySNIs.filter((d) => d !== domain),
    );
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      handleAddDomain();
    }
  };

  return (
    <>
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Combo Strategy
        </span>
      </div>

      <div>
        <Alert>
          <AlertDescription>
            Combo combines multiple split points and sends segments out of order
            with timing jitter to confuse stateful DPI.
          </AlertDescription>
        </Alert>
      </div>

      {/* Decoy Settings */}
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Decoy Packet
        </span>
      </div>

      <div>
        <label htmlFor="switch-combo-decoy">
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
        </label>
      </div>

      {combo.decoy_enabled && (
        <>
          <div className="md:col-span-2">
            <Field>
              <FieldLabel>Decoy SNI Domains</FieldLabel>
              <FieldDescription className="mb-2">
                Whitelisted domains used in decoy packet. DPI sees these instead
                of real target.
              </FieldDescription>
              <div className="flex gap-2">
                <Input
                  value={newDomain}
                  onChange={(e) => setNewDomain(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="e.g., allowed-site.ru"
                  className="flex-1"
                />
                <Button
                  onClick={handleAddDomain}
                  disabled={!newDomain.trim()}
                  size="icon"
                  variant="outline"
                >
                  <AddIcon />
                </Button>
              </div>
              <div className="mt-2">
                <ChipList
                  items={decoySNIs}
                  getKey={(d) => d}
                  getLabel={(d) => d}
                  onDelete={handleRemoveDomain}
                  emptyMessage="Using defaults: ya.ru, vk.com, mail.ru, dzen.ru"
                  showEmpty
                />
              </div>
            </Field>
          </div>

          {/* How Decoy Works Visualization */}
          <div className="md:col-span-2">
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
          </div>
        </>
      )}

      {/* Split Points */}
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Split Points
        </span>
      </div>

      <div>
        <label htmlFor="switch-combo-first-byte">
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>First Byte</FieldTitle>
              <FieldDescription>
                Split after 1st byte (timing desync)
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-combo-first-byte"
              checked={combo.first_byte_split}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.combo.first_byte_split", checked)
              }
            />
          </Field>
        </label>
      </div>

      <div>
        <label htmlFor="switch-combo-extension">
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Extension Split</FieldTitle>
              <FieldDescription>Split before SNI extension</FieldDescription>
            </FieldContent>
            <Switch
              id="switch-combo-extension"
              checked={combo.extension_split}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.combo.extension_split", checked)
              }
            />
          </Field>
        </label>
      </div>

      <div>
        <label htmlFor="switch-combo-sni">
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>SNI Split</FieldTitle>
              <FieldDescription>
                Split in middle of SNI hostname
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-combo-sni"
              checked={middleSni}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.middle_sni", checked)
              }
            />
          </Field>
        </label>
      </div>

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

      {/* Shuffle Mode */}
      <div>
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
            How to reorder segments before sending
          </FieldDescription>
        </Field>
      </div>

      <div>
        <Alert className="my-0">
          <AlertDescription>
            {combo.shuffle_mode === "middle" &&
              "Middle: Keep first & last in place, shuffle middle segments"}
            {combo.shuffle_mode === "full" &&
              "Full: Randomly shuffle all segments"}
            {combo.shuffle_mode === "reverse" &&
              "Reverse: Send segments in reverse order"}
          </AlertDescription>
        </Alert>
      </div>

      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Timing Settings
        </span>
      </div>

      <div>
        <Field className="w-full space-y-2">
          <div className="flex items-center justify-between">
            <FieldLabel className="text-sm font-medium">
              First Segment Delay
            </FieldLabel>
            <Badge variant="secondary" className="font-semibold">
              {combo.first_delay_ms} ms
            </Badge>
          </div>
          <Slider
            value={[combo.first_delay_ms]}
            onValueChange={(values) =>
              onChange("fragmentation.combo.first_delay_ms", values[0])
            }
            min={10}
            max={500}
            step={10}
            className="w-full"
          />
          <FieldDescription>Delay after first segment (ms)</FieldDescription>
        </Field>
      </div>

      <div>
        <Field className="w-full space-y-2">
          <div className="flex items-center justify-between">
            <FieldLabel className="text-sm font-medium">Jitter Max</FieldLabel>
            <Badge variant="secondary" className="font-semibold">
              {combo.jitter_max_us} μs
            </Badge>
          </div>
          <Slider
            value={[combo.jitter_max_us]}
            onValueChange={(values) =>
              onChange("fragmentation.combo.jitter_max_us", values[0])
            }
            min={100}
            max={10000}
            step={100}
            className="w-full"
          />
          <FieldDescription>
            Max random delay between other segments (μs)
          </FieldDescription>
        </Field>
      </div>

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

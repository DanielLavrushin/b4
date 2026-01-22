import { Capture } from "@b4.capture";
import { ComboboxMultiple } from "@composed/combobox-multiple";
import { Alert, AlertDescription } from "@design/primitives/alert";
import { Separator } from "@design/primitives/separator";
import { Slider } from "@design/primitives/slider";
import { Badge } from "@primitives/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@primitives/collapsible";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import { Switch } from "@primitives/switch";
import { useEffect, useState } from "react";

export interface DiscoveryOptions {
  skipDNS: boolean;
  payloadFiles: string[];
  validationTries: number;
}

interface DiscoveryOptionsPanelProps {
  options: DiscoveryOptions;
  onChange: (options: DiscoveryOptions) => void;
  captures: Capture[];
  disabled?: boolean;
}

export const DiscoveryOptionsPanel = ({
  options,
  onChange,
  captures,
  disabled = false,
}: DiscoveryOptionsPanelProps) => {
  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem("b4_discovery_options_expanded") === "true";
  });

  useEffect(() => {
    localStorage.setItem("b4_discovery_options_expanded", String(expanded));
  }, [expanded]);

  const tlsCaptures = captures.filter((c) => c.protocol === "tls");
  const hasOptions =
    options.skipDNS ||
    options.payloadFiles.length > 0 ||
    options.validationTries > 1;

  return (
    <Collapsible open={expanded} onOpenChange={setExpanded}>
      <Card>
        <CollapsibleTrigger className="cursor-pointer">
          <CardHeader>
            <CardTitle>Discovery Options</CardTitle>
            <CardDescription>{getOptionsSummary(options)}</CardDescription>
          </CardHeader>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <>
            <Separator />
            <CardContent>
              <FieldSet>
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldTitle>Skip DNS Discovery</FieldTitle>
                    <FieldDescription>
                      Skip DNS detection phase for faster results
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    id="switch-skip-dns"
                    checked={options.skipDNS}
                    onCheckedChange={(checked) =>
                      onChange({ ...options, skipDNS: checked })
                    }
                    disabled={disabled}
                  />
                </Field>

                {/* Validation Tries */}
                <Field>
                  <FieldLabel>
                    Validation Tries
                    <Badge variant="secondary">{options.validationTries}</Badge>
                  </FieldLabel>
                  <Slider
                    value={[options.validationTries]}
                    onValueChange={(value) => {
                      const values = Array.isArray(value) ? value : [value];
                      onChange({ ...options, validationTries: values[0] });
                    }}
                    min={1}
                    max={5}
                    step={1}
                  />
                  <FieldDescription>
                    Number of successful connection attempts required to
                    validate a preset
                  </FieldDescription>
                </Field>

                {/* Custom Payloads */}
                {tlsCaptures.length > 0 && (
                  <div className="md:col-span-2">
                    <Field>
                      <FieldLabel>Custom Payloads</FieldLabel>
                      <FieldDescription>
                        Test with captured TLS ClientHello instead of built-in
                        payloads
                      </FieldDescription>
                      <ComboboxMultiple
                        items={tlsCaptures.map((c) => c.domain)}
                        value={options.payloadFiles}
                        onValueChange={(values) =>
                          onChange({ ...options, payloadFiles: values })
                        }
                        placeholder="Search captured payloads..."
                        emptyMessage="No captured payloads found."
                        disabled={disabled}
                      />
                    </Field>
                  </div>
                )}

                {tlsCaptures.length === 0 && (
                  <Field>
                    <Alert>
                      <AlertDescription>
                        No captured payloads available.{" "}
                        <a
                          href="/settings#capture"
                          className="text-primary hover:underline"
                        >
                          Capture payloads
                        </a>{" "}
                        to test with custom TLS ClientHello.
                      </AlertDescription>
                    </Alert>
                  </Field>
                )}
              </FieldSet>
            </CardContent>
          </>
        </CollapsibleContent>
      </Card>
    </Collapsible>
  );
};

function getOptionsSummary(options: DiscoveryOptions): string {
  const parts: string[] = [];
  if (options.skipDNS) parts.push("Skip DNS");
  if (options.validationTries > 1)
    parts.push(`${options.validationTries} tries`);
  if (options.payloadFiles.length > 0) {
    parts.push(
      `${options.payloadFiles.length} payload${
        options.payloadFiles.length > 1 ? "s" : ""
      }`,
    );
  }
  return parts.join(", ");
}

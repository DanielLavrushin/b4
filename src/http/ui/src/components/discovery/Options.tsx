import { useState, useEffect } from "react";
import { CollapseIcon, ExpandIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@design/components/ui/collapsible";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldTitle,
} from "@design/components/ui/field";
import { Separator } from "@design/components/ui/separator";
import { Switch } from "@design/components/ui/switch";
import { Capture } from "@b4.capture";
import { ChipList } from "@components/common/ChipList";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@design/components/ui/select";

export interface DiscoveryOptions {
  skipDNS: boolean;
  payloadFiles: string[];
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
  const hasOptions = options.skipDNS || options.payloadFiles.length > 0;

  const handleAddPayload = (domain: string) => {
    if (!options.payloadFiles.includes(domain)) {
      onChange({ ...options, payloadFiles: [...options.payloadFiles, domain] });
    }
  };

  const handleRemovePayload = (domain: string) => {
    onChange({
      ...options,
      payloadFiles: options.payloadFiles.filter((d) => d !== domain),
    });
  };

  return (
    <div className="border-border overflow-hidden rounded-md border">
      {/* Header */}
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <CollapsibleTrigger asChild>
          <div className="bg-accent hover:bg-accent/80 flex cursor-pointer items-center justify-between p-3 transition-colors">
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground text-sm">
                Discovery Options
              </span>
              {!expanded && hasOptions && (
                <Badge variant="secondary" className="text-xs">
                  {getOptionsSummary(options)}
                </Badge>
              )}
            </div>
            {expanded ? (
              <CollapseIcon className="text-muted-foreground h-4 w-4" />
            ) : (
              <ExpandIcon className="text-muted-foreground h-4 w-4" />
            )}
          </div>
        </CollapsibleTrigger>

        {/* Content */}
        <CollapsibleContent>
          <div className="bg-card border-border space-y-4 border-t p-4">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {/* Skip DNS Switch */}
              <label htmlFor="switch-skip-dns">
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
              </label>

              {/* Custom Payloads */}
              {tlsCaptures.length > 0 && (
                <div className="md:col-span-2">
                  <Field>
                    <FieldLabel>Custom Payloads</FieldLabel>
                    <FieldDescription className="mb-2">
                      Test with captured TLS ClientHello instead of built-in
                      payloads
                    </FieldDescription>
                    <Select
                      onValueChange={handleAddPayload}
                      disabled={disabled}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select captured payloads..." />
                      </SelectTrigger>
                      <SelectContent>
                        {tlsCaptures
                          .filter(
                            (c) => !options.payloadFiles.includes(c.domain),
                          )
                          .map((capture) => (
                            <SelectItem
                              key={capture.domain}
                              value={capture.domain}
                            >
                              {capture.domain}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                    {options.payloadFiles.length > 0 && (
                      <div className="mt-2">
                        <ChipList
                          items={options.payloadFiles}
                          getKey={(d) => d}
                          getLabel={(d) => d}
                          onDelete={handleRemovePayload}
                          emptyMessage="No payloads selected"
                        />
                      </div>
                    )}
                  </Field>
                </div>
              )}

              {tlsCaptures.length === 0 && (
                <div className="md:col-span-2">
                  <p className="text-muted-foreground text-xs">
                    No captured payloads available.{" "}
                    <a
                      href="/settings#capture"
                      className="text-primary hover:underline"
                    >
                      Capture payloads
                    </a>{" "}
                    to test with custom TLS ClientHello.
                  </p>
                </div>
              )}
            </div>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
};

function getOptionsSummary(options: DiscoveryOptions): string {
  const parts: string[] = [];
  if (options.skipDNS) parts.push("Skip DNS");
  if (options.payloadFiles.length > 0) {
    parts.push(
      `${options.payloadFiles.length} payload${
        options.payloadFiles.length > 1 ? "s" : ""
      }`,
    );
  }
  return parts.join(", ");
}

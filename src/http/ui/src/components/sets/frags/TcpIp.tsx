import { B4SetConfig } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
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
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";

interface TcpIpSettingsProps {
  config: B4SetConfig;
  onChange: (field: string, value: string | boolean | number) => void;
}

export const TcpIpSettings = ({ config, onChange }: TcpIpSettingsProps) => {
  const getSplitModeDescription = () => {
    if (config.fragmentation.middle_sni) {
      if (config.fragmentation.sni_position > 0) {
        return "3 segments: split at fixed position AND middle of SNI";
      }
      return "2 segments: split at middle of SNI hostname";
    }
    return `2 segments: split at byte ${config.fragmentation.sni_position} of TLS payload`;
  };

  return (
    <>
      <FieldSeparator className="my-6">Tcp/Ip Fragmentation</FieldSeparator>

      <FieldGroup>
        <div className="flex flex-row gap-4">
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Smart SNI Split</FieldTitle>
              <FieldDescription>
                Automatically split in the middle of the SNI hostname
                (recommended)
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-tcpip-middle-sni"
              checked={config.fragmentation.middle_sni}
              onCheckedChange={(checked: boolean) =>
                onChange("fragmentation.middle_sni", checked)
              }
            />
          </Field>
          <Field>
            <FieldLabel>
              Fixed Split Position
              <Badge variant="secondary">
                {config.fragmentation.sni_position}
              </Badge>
            </FieldLabel>
            <Slider
              value={[config.fragmentation.sni_position]}
              onValueChange={(val) =>
                onChange("fragmentation.sni_position", val as number)
              }
              min={0}
              max={50}
              step={1}
            />
            <FieldDescription>
              Bytes from TLS payload start (0 = disabled)
            </FieldDescription>
          </Field>
        </div>

        {/* Visual explanation */}
        <Field>
          <div className="md:col-span-2">
            <div className="bg-card border-border border p-4">
              <p className="text-muted-foreground mb-2 text-xs">
                TCP PACKET STRUCTURE EXAMPLE
              </p>
              <div className="flex gap-1 font-mono text-xs">
                <div className="bg-accent min-w-15 p-2 text-center">
                  TLS Header
                </div>
                <div className="relative flex-1 p-2 text-center">
                  {/* Fixed position split line */}
                  {config.fragmentation.sni_position > 0 && (
                    <span className="absolute top-0 bottom-0 left-[20%] w-0.5 -translate-x-1/2" />
                  )}
                  {/* Middle SNI split line */}
                  {config.fragmentation.middle_sni && (
                    <span className="absolute top-0 bottom-0 left-1/2 w-0.5 -translate-x-1/2" />
                  )}
                  SNI: youtube.com
                </div>
                <div className="bg-accent min-w-20 p-2 text-center">
                  Extensions...
                </div>
              </div>
              <p className="text-muted-foreground mt-2 text-xs">
                {getSplitModeDescription()}
              </p>
            </div>
          </div>
        </Field>
      </FieldGroup>

      {config.fragmentation.sni_position > 0 &&
        config.fragmentation.middle_sni && (
          <Alert className="mt-4">
            <AlertDescription>
              Both enabled → packet splits into 3 segments
            </AlertDescription>
          </Alert>
        )}
    </>
  );
};

import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldTitle,
} from "@primitives/field";
import { Separator } from "@primitives/separator";
import { Slider } from "@primitives/slider";
import { Switch } from "@primitives/switch";
import { B4SetConfig } from "@models/config";

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
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Where to Split
        </span>
      </div>

      <div className="md:col-span-2">
        <label htmlFor="switch-tcpip-middle-sni">
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
        </label>
      </div>

      {/* Visual explanation */}
      <div className="md:col-span-2">
        <div className="bg-card border-border rounded-md border p-4">
          <p className="text-muted-foreground mb-2 text-xs">
            TCP PACKET STRUCTURE EXAMPLE
          </p>
          <div className="flex gap-1 font-mono text-xs">
            <div className="bg-accent min-w-15 rounded p-2 text-center">
              TLS Header
            </div>
            <div className="bg-accent-secondary relative flex-1 rounded p-2 text-center">
              {/* Fixed position split line */}
              {config.fragmentation.sni_position > 0 && (
                <span className="bg-tertiary absolute top-0 bottom-0 left-[20%] w-0.5 -translate-x-1/2" />
              )}
              {/* Middle SNI split line */}
              {config.fragmentation.middle_sni && (
                <span className="bg-quaternary absolute top-0 bottom-0 left-1/2 w-0.5 -translate-x-1/2" />
              )}
              SNI: youtube.com
            </div>
            <div className="bg-accent min-w-20 rounded p-2 text-center">
              Extensions...
            </div>
          </div>
          <p className="text-muted-foreground mt-2 text-xs">
            {getSplitModeDescription()}
          </p>
        </div>
      </div>

      <div className="md:col-span-2">
        <p className="text-warning mb-2 text-xs">
          Manual override — use if Smart SNI Split doesn't work for your ISP
        </p>
        <div className="mt-2">
          <Field className="w-full space-y-2">
            <div className="flex items-center justify-between">
              <FieldLabel className="text-sm font-medium">
                Fixed Split Position
              </FieldLabel>
              <Badge variant="secondary" className="font-semibold">
                {config.fragmentation.sni_position}
              </Badge>
            </div>
            <Slider
              value={[config.fragmentation.sni_position]}
              onValueChange={(values) =>
                onChange("fragmentation.sni_position", values[0])
              }
              min={0}
              max={50}
              step={1}
              className="w-full"
            />
            <FieldDescription>
              Bytes from TLS payload start (0 = disabled)
            </FieldDescription>
          </Field>
        </div>
        {config.fragmentation.sni_position > 0 &&
          config.fragmentation.middle_sni && (
            <Alert className="mt-4">
              <AlertDescription>
                Both enabled → packet splits into 3 segments
              </AlertDescription>
            </Alert>
          )}
      </div>
    </>
  );
};

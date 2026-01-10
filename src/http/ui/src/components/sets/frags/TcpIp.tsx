import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldTitle,
} from "@design/components/ui/field";
import { Separator } from "@design/components/ui/separator";
import { Slider } from "@design/components/ui/slider";
import { Switch } from "@design/components/ui/switch";
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
      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
          Where to Split
        </span>
      </div>

      <div className="md:col-span-2">
        <label htmlFor="switch-tcpip-middle-sni">
          <Field orientation="horizontal" className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2">
            <FieldContent>
              <FieldTitle>Smart SNI Split</FieldTitle>
              <FieldDescription>
                Automatically split in the middle of the SNI hostname (recommended)
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
        <div className="p-4 bg-card rounded-md border border-border">
          <p className="text-xs text-muted-foreground mb-2">
            TCP PACKET STRUCTURE EXAMPLE
          </p>
          <div className="flex gap-1 font-mono text-xs">
            <div className="p-2 bg-accent rounded text-center min-w-15">
              TLS Header
            </div>
            <div className="p-2 bg-accent-secondary rounded text-center flex-1 relative">
              {/* Fixed position split line */}
              {config.fragmentation.sni_position > 0 && (
                <span className="absolute left-[20%] top-0 bottom-0 w-0.5 bg-tertiary -translate-x-1/2" />
              )}
              {/* Middle SNI split line */}
              {config.fragmentation.middle_sni && (
                <span className="absolute left-1/2 top-0 bottom-0 w-0.5 bg-quaternary -translate-x-1/2" />
              )}
              SNI: youtube.com
            </div>
            <div className="p-2 bg-accent rounded text-center min-w-20">
              Extensions...
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            {getSplitModeDescription()}
          </p>
        </div>
      </div>

      <div className="md:col-span-2">
        <p className="text-xs text-warning mb-2">
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

import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Separator } from "@design/components/ui/separator";
import { B4SetConfig } from "@models/config";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  return (
    <>
      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
          First-Byte Desync
        </span>
      </div>

      <Alert className="m-0">
        <AlertDescription>
          Sends just 1 byte, waits, then sends the rest. Exploits DPI timeout —
          incomplete TLS record can't be parsed.
        </AlertDescription>
      </Alert>

      <div className="md:col-span-2">
        <div className="p-4 bg-card rounded-md border border-border">
          <p className="text-xs text-muted-foreground mb-2">TIMING ATTACK</p>
          <div className="flex items-center gap-2 font-mono text-xs">
            <div className="p-2 bg-tertiary rounded min-w-10 text-center">
              0x16
            </div>
            <div className="flex flex-col items-center text-muted-foreground">
              <p className="text-xs">⏱️ {config.tcp.seg2delay || 30}ms+</p>
              <div className="w-15 h-0.5 bg-quaternary my-1" />
            </div>
            <div className="p-2 bg-accent-secondary rounded flex-1 text-center">
              Rest of TLS ClientHello...
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
            DPI sees 1 byte (TLS record type), waits for more, times out before
            SNI arrives
          </p>
        </div>
      </div>

      <div className="md:col-span-2">
        <Alert className="m-0">
          <AlertDescription>
            No configuration needed. Delay controlled by{" "}
            <strong>Seg2 Delay</strong> in TCP tab (minimum 100ms applied
            automatically).
          </AlertDescription>
        </Alert>
      </div>
    </>
  );
};

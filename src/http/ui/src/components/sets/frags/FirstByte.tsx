import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Separator } from "@design/components/ui/separator";
import { B4SetConfig } from "@models/config";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  return (
    <>
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
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
        <div className="bg-card border-border rounded-md border p-4">
          <p className="text-muted-foreground mb-2 text-xs">TIMING ATTACK</p>
          <div className="flex items-center gap-2 font-mono text-xs">
            <div className="bg-tertiary min-w-10 rounded p-2 text-center">
              0x16
            </div>
            <div className="text-muted-foreground flex flex-col items-center">
              <p className="text-xs">⏱️ {config.tcp.seg2delay || 30}ms+</p>
              <div className="bg-quaternary my-1 h-0.5 w-15" />
            </div>
            <div className="bg-accent-secondary flex-1 rounded p-2 text-center">
              Rest of TLS ClientHello...
            </div>
          </div>
          <p className="text-muted-foreground mt-2 text-xs">
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

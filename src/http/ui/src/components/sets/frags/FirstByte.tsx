import {
  FieldDescription,
  FieldLegend,
  FieldSet,
} from "@design/primitives/field";
import { B4SetConfig } from "@models/config";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  return (
    <FieldSet>
      <FieldLegend>First-Byte Desync</FieldLegend>
      <FieldDescription>
        Sends just 1 byte, waits, then sends the rest. Exploits DPI timeout —
        incomplete TLS record can't be parsed.
      </FieldDescription>

      <div className="md:col-span-2">
        <div className="bg-card border-border rounded-md border p-4">
          <p className="text-muted-foreground mb-2 text-xs">TIMING ATTACK</p>
          <div className="flex items-center gap-2 font-mono text-xs">
            <div className="bg-tertiary min-w-10 rounded p-2 text-center">
              0x16
            </div>
            <div className="text-muted-foreground flex flex-col items-center justify-center">
              <p className="text-xs">{config.tcp.seg2delay || 30}ms+</p>
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
    </FieldSet>
  );
};

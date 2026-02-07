import { Alert, AlertDescription } from "@design/primitives/alert";
import { Field, FieldGroup, FieldSeparator } from "@design/primitives/field";
import { B4SetConfig } from "@models/config";

interface FirstByteSettingsProps {
  config: B4SetConfig;
}

export const FirstByteSettings = ({ config }: FirstByteSettingsProps) => {
  return (
    <>
      <FieldSeparator className="my-6">First-Byte Desync</FieldSeparator>

      <FieldGroup>
        <Field>
          <Alert>
            <AlertDescription className="text-center">
              Sends just 1 byte, waits, then sends the rest. Exploits DPI
              timeout — incomplete TLS record can't be parsed.
            </AlertDescription>
          </Alert>
        </Field>

        <Field>
          <div className="md:col-span-2">
            <div className="bg-card border-border border p-4">
              <p className="text-muted-foreground mb-2 text-xs">
                TIMING ATTACK
              </p>
              <div className="flex items-center gap-2 font-mono text-xs">
                <div className="min-w-10 p-2 text-center">0x16</div>
                <div className="text-muted-foreground flex items-center justify-center">
                  <p className="text-center text-xs">
                    {config.tcp.seg2delay || 30}ms+
                  </p>
                  <div className="my-1 h-0.5 w-15" />
                </div>
                <div className="flex-1 p-2 text-center">
                  Rest of TLS ClientHello...
                </div>
              </div>
              <p className="text-muted-foreground mt-2 text-xs">
                DPI sees 1 byte (TLS record type), waits for more, times out
                before SNI arrives
              </p>
            </div>
          </div>
        </Field>
      </FieldGroup>
    </>
  );
};

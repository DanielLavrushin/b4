import { Alert, AlertDescription } from "@design/primitives/alert";
import { Field, FieldGroup, FieldSeparator } from "@design/primitives/field";

export const ExtSplitSettings = () => {
  return (
    <>
      <FieldSeparator className="my-6">Extension Split</FieldSeparator>
      <FieldGroup>
        <Field>
          <Alert>
            <AlertDescription className="text-center">
              Automatically splits TLS ClientHello just before the SNI
              extension. DPI sees incomplete extension list and fails to parse
              SNI.
            </AlertDescription>
          </Alert>
        </Field>
        <Field>
          <div className="md:col-span-2">
            <div className="bg-card border-border border p-4">
              <p className="text-muted-foreground mb-2 text-xs">
                TLS CLIENTHELLO STRUCTURE
              </p>
              <div className="flex flex-wrap gap-1 font-mono text-xs">
                <div className="bg-accent p-2">TLS Header</div>
                <div className="bg-accent p-2">Handshake</div>
                <div className="bg-accent p-2">Ciphers</div>
                <div className="p-2">Ext₁</div>
                <div className="p-2">Ext₂</div>
                <div className="p-2">SNI: youtube.com</div>
                <div className="p-2">Ext...</div>
              </div>
              <p className="text-muted-foreground mt-2 text-xs">
                Split happens before SNI extension starts
              </p>
            </div>
          </div>
        </Field>
      </FieldGroup>
    </>
  );
};

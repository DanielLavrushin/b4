import {
  FieldDescription,
  FieldLegend,
  FieldSet,
} from "@design/primitives/field";

export const ExtSplitSettings = () => {
  return (
    <FieldSet>
      <FieldLegend>Extension Split</FieldLegend>
      <FieldDescription>
        Automatically splits TLS ClientHello just before the SNI extension. DPI
        sees incomplete extension list and fails to parse SNI.
      </FieldDescription>

      <div className="md:col-span-2">
        <div className="bg-card border-border rounded-md border p-4">
          <p className="text-muted-foreground mb-2 text-xs">
            TLS CLIENTHELLO STRUCTURE
          </p>
          <div className="flex flex-wrap gap-1 font-mono text-xs">
            <div className="bg-accent rounded p-2">TLS Header</div>
            <div className="bg-accent rounded p-2">Handshake</div>
            <div className="bg-accent rounded p-2">Ciphers</div>
            <div className="bg-accent-secondary rounded p-2">Ext₁</div>
            <div className="bg-accent-secondary rounded p-2">Ext₂</div>
            <div className="bg-tertiary relative rounded p-2">
              <span className="bg-quaternary absolute top-0 bottom-0 -left-2 w-0.75" />
              SNI: youtube.com
            </div>
            <div className="bg-accent-secondary rounded p-2">Ext...</div>
          </div>
          <p className="text-muted-foreground mt-2 text-xs">
            Split happens before SNI extension starts
          </p>
        </div>
      </div>
    </FieldSet>
  );
};

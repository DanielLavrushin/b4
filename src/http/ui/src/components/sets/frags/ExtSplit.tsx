import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Separator } from "@design/components/ui/separator";

export const ExtSplitSettings = () => {
  return (
    <>
      <div className="relative my-4 flex items-center md:col-span-2">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-muted-foreground bg-card relative mx-auto block w-fit px-2 text-xs font-medium uppercase">
          Extension Split
        </span>
      </div>
      <Alert className="m-0">
        <AlertDescription>
          Automatically splits TLS ClientHello just before the SNI extension.
          DPI sees incomplete extension list and fails to parse SNI.
        </AlertDescription>
      </Alert>

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
            Split happens at the yellow line — before SNI extension starts
          </p>
        </div>
      </div>

      <div className="md:col-span-2">
        <Alert className="m-0">
          <AlertDescription>
            No configuration needed. Uses <strong>Reverse Order</strong> toggle
            above and <strong>Seg2 Delay</strong> from TCP tab.
          </AlertDescription>
        </Alert>
      </div>
    </>
  );
};

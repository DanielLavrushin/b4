import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Separator } from "@design/components/ui/separator";

export const ExtSplitSettings = () => {
  return (
    <>
      <div className="relative my-4 md:col-span-2 flex items-center">
        <Separator className="absolute inset-0 top-1/2" />
        <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
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
        <div className="p-4 bg-card rounded-md border border-border">
          <p className="text-xs text-muted-foreground mb-2">
            TLS CLIENTHELLO STRUCTURE
          </p>
          <div className="flex gap-1 font-mono text-xs flex-wrap">
            <div className="p-2 bg-accent rounded">TLS Header</div>
            <div className="p-2 bg-accent rounded">Handshake</div>
            <div className="p-2 bg-accent rounded">Ciphers</div>
            <div className="p-2 bg-accent-secondary rounded">Ext₁</div>
            <div className="p-2 bg-accent-secondary rounded">Ext₂</div>
            <div className="p-2 bg-tertiary rounded relative">
              <span className="absolute -left-2 top-0 bottom-0 w-0.75 bg-quaternary" />
              SNI: youtube.com
            </div>
            <div className="p-2 bg-accent-secondary rounded">Ext...</div>
          </div>
          <p className="text-xs text-muted-foreground mt-2">
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

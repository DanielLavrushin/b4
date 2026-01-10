import { AddIcon, InfoIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import { useEffect, useState } from "react";

interface IpInfo {
  ip: string;
  hostname?: string;
  city?: string;
  region?: string;
  country?: string;
  loc?: string;
  org?: string;
  postal?: string;
  timezone?: string;
}

interface IpInfoModalProps {
  open: boolean;
  ip: string;
  token: string;
  onClose: () => void;
  onAddHostname?: (hostname: string) => void;
}

export const IpInfoModal = ({
  open,
  ip,
  token,
  onClose,
  onAddHostname,
}: IpInfoModalProps) => {
  const [ipInfo, setIpInfo] = useState<IpInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !ip || !token) return;

    const fetchIpInfo = async () => {
      setLoading(true);
      setError(null);
      try {
        const cleanIp = ip.split(":")[0].replace(/[[\]]/g, "");
        const response = await fetch(
          `/api/integration/ipinfo?ip=${encodeURIComponent(cleanIp)}`
        );
        if (!response.ok) throw new Error("Failed to fetch IP info");
        const data = (await response.json()) as IpInfo;
        setIpInfo(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unknown error");
      } finally {
        setLoading(false);
      }
    };

    void fetchIpInfo();
  }, [open, ip, token]);

  const handleAddHostname = () => {
    if (ipInfo?.hostname && onAddHostname) {
      onAddHostname(ipInfo.hostname);
      onClose();
    }
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <InfoIcon />
            </div>
            <div className="flex-1">
              <DialogTitle>IP Information</DialogTitle>
            </div>
          </div>
        </DialogHeader>
        <div className="py-4">
          <>
            {loading && (
              <div className="flex justify-center py-8">
                <Spinner />
              </div>
            )}

            {error && (
              <Alert variant="destructive" className="mb-4">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            {ipInfo && !loading && (
              <div className="flex flex-col gap-4">
                {ipInfo.org && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Organization</p>
                    <p className="text-sm">
                      <a
                        href={"https://ipinfo.io/" + ipInfo.org.split(" ")[0]}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary hover:underline"
                      >
                        {ipInfo.org}
                      </a>
                    </p>
                  </div>
                )}

                {ipInfo.hostname && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Hostname</p>
                    <p className="text-sm font-mono">
                      <Badge variant="secondary">{ipInfo.hostname}</Badge>
                    </p>
                  </div>
                )}

                <div>
                  <p className="text-xs text-secondary mb-1">IP Address</p>
                  <p className="text-sm font-mono">
                    <a
                      href={"https://ipinfo.io/" + ipInfo.ip}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary hover:underline"
                    >
                      {ipInfo.ip}
                    </a>
                  </p>
                </div>

                {(ipInfo.city || ipInfo.region || ipInfo.country) && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Location</p>
                    <p className="text-sm">
                      {[ipInfo.city, ipInfo.region, ipInfo.country]
                        .filter(Boolean)
                        .join(", ")}
                    </p>
                  </div>
                )}

                {ipInfo.loc && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Coordinates</p>
                    <p className="text-sm font-mono">{ipInfo.loc}</p>
                  </div>
                )}

                {ipInfo.timezone && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Timezone</p>
                    <p className="text-sm">{ipInfo.timezone}</p>
                  </div>
                )}

                {ipInfo.postal && (
                  <div>
                    <p className="text-xs text-secondary mb-1">Postal Code</p>
                    <p className="text-sm">{ipInfo.postal}</p>
                  </div>
                )}
              </div>
            )}
          </>
        </div>
        <Separator />
        <DialogFooter>
          {ipInfo?.hostname && onAddHostname && (
            <Button onClick={handleAddHostname}>
              <AddIcon className="h-4 w-4 mr-2" />
              Add Hostname
            </Button>
          )}
          <div className="flex-1" />
          <Button onClick={onClose} variant="outline">
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

import { AddIcon, DomainIcon } from "@b4.icons";
import { SetSelector } from "@common/SetSelector";
import { clearAsnLookupCache } from "@hooks/useDomainActions";
import { B4SetConfig, MAIN_SET_ID } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Card, CardContent } from "@primitives/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldTitle,
} from "@primitives/field";
import { Label } from "@primitives/label";
import { RadioGroup, RadioGroupItem } from "@primitives/radio-group";
import { Separator } from "@primitives/separator";
import { asnStorage } from "@utils";
import { useCallback, useEffect, useState } from "react";

interface IpInfo {
  ip: string;
  hostname?: string;
  org?: string;
  city?: string;
  region?: string;
  country?: string;
}

interface RipeNetworkInfo {
  asns: string[];
  prefix: string;
}

interface AddIpModalProps {
  open: boolean;
  ip: string;
  variants: string[];
  sets: B4SetConfig[];
  selected: string;
  ipInfoToken?: string;
  onClose: () => void;
  onSelectVariant: (variant: string | string[]) => void;
  onAdd: (setId: string, setName?: string) => void;
  onAddHostname?: (hostname: string) => void;
}

export const AddIpModal = ({
  open,
  ip,
  sets,
  variants: initialVariants,
  selected,
  ipInfoToken,
  onClose,
  onSelectVariant,
  onAdd,
  onAddHostname,
}: AddIpModalProps) => {
  const [selectedSetId, setSelectedSetId] = useState<string>("");
  const [ipInfo, setIpInfo] = useState<IpInfo | null>(null);
  const [loadingInfo, setLoadingInfo] = useState(false);
  const [loadingPrefixes, setLoadingPrefixes] = useState(false);
  const [variants, setVariants] = useState<string[]>(initialVariants);
  const [asn, setAsn] = useState<string>("");
  const [prefixes, setPrefixes] = useState<string[]>([]);
  const [addMode, setAddMode] = useState<"single" | "all">("single");
  const [newSetName, setNewSetName] = useState<string>("");

  useEffect(() => {
    if (open) {
      setIpInfo(null);
      setAsn("");
      setPrefixes([]);
      setAddMode("single");
      setLoadingInfo(false);
      setLoadingPrefixes(false);
      setNewSetName("");
      setVariants(initialVariants);
      if (sets.length > 0) {
        setSelectedSetId(MAIN_SET_ID);
      }
    }
  }, [open, sets, initialVariants, ip]);

  useEffect(() => {
    if (!open) {
      setIpInfo(null);
      setAsn("");
      setPrefixes([]);
      setVariants(initialVariants);
      setAddMode("single");
      setLoadingInfo(false);
      setLoadingPrefixes(false);
      setNewSetName("");
      onSelectVariant(initialVariants[0] || "");
    }
  }, [open, initialVariants, onSelectVariant]);

  const loadIpInfo = async () => {
    setLoadingInfo(true);
    try {
      const cleanIp = ip.split(":")[0].replace(/[[\]]/g, "");
      const response = await fetch(
        `/api/integration/ipinfo?ip=${encodeURIComponent(cleanIp)}`,
      );
      if (response.ok) {
        const data = (await response.json()) as IpInfo;
        setIpInfo(data);

        // Extract ASN from org field (e.g., "AS13335 Cloudflare, Inc.")
        if (data.org) {
          const asnMatch = data.org.match(/AS(\d+)/);
          if (asnMatch) {
            setAsn(asnMatch[1]);
          }
        }
      }
    } catch (error) {
      console.error("Failed to load IP info:", error);
    } finally {
      setLoadingInfo(false);
    }
  };

  const loadRipeNetworkInfo = async () => {
    setLoadingInfo(true);
    try {
      const cleanIp = ip.split(":")[0].replace(/[[\]]/g, "");
      const response = await fetch(
        `/api/integration/ripestat?ip=${encodeURIComponent(cleanIp)}`,
      );
      if (response.ok) {
        const data = (await response.json()) as { data: RipeNetworkInfo };
        const networkData = data.data;
        if (networkData.asns && networkData.asns.length > 0) {
          setAsn(networkData.asns[0]);
          setIpInfo({
            ip: cleanIp,
            org: `AS${networkData.asns[0]}`,
          });
        }
      }
    } catch (error) {
      console.error("Failed to load RIPE network info:", error);
    } finally {
      setLoadingInfo(false);
    }
  };

  const loadRipePrefixes = useCallback(async () => {
    if (!asn) return;

    setLoadingPrefixes(true);
    try {
      const response = await fetch(
        `/api/integration/ripestat/asn?asn=${encodeURIComponent(asn)}`,
      );
      if (response.ok) {
        const data = (await response.json()) as {
          data: { prefixes: Array<{ prefix: string }> };
        };
        const loadedPrefixes = data.data.prefixes.map((p) => p.prefix);
        setPrefixes(loadedPrefixes);
        setAddMode("all");
        onSelectVariant(loadedPrefixes);
        asnStorage.addAsn(asn, ipInfo?.org || `AS${asn}`, loadedPrefixes);
        clearAsnLookupCache();
      }
    } catch (error) {
      console.error("Failed to load RIPE prefixes:", error);
    } finally {
      setLoadingPrefixes(false);
    }
  }, [asn, ipInfo?.org, onSelectVariant]);

  const handleAdd = () => {
    onAdd(selectedSetId, newSetName);
  };

  useEffect(() => {
    if (asn && open) {
      void loadRipePrefixes();
    }
  }, [asn, loadRipePrefixes, open]);

  const handleAddHostname = () => {
    if (ipInfo?.hostname && onAddHostname) {
      onAddHostname(ipInfo.hostname);
      onClose();
    }
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <Button variant="secondary" size="icon" className="shrink-0">
              <DomainIcon />
            </Button>
            <DialogTitle>Add IP/CIDR to Manual List</DialogTitle>
          </div>
        </DialogHeader>
        <div className="space-y-4">
          <Alert>
            <AlertDescription>
              Select the desired IP or CIDR range. You can enrich with network
              information to load all ASN prefixes.
            </AlertDescription>
          </Alert>

          <div className="space-y-4">
            {!ipInfo ? (
              <>
                <p className="text-muted-foreground text-sm">
                  Original IP: <Badge>{ip}</Badge>
                </p>
                <div className="flex flex-col gap-2">
                  {ipInfoToken && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => void loadIpInfo()}
                      disabled={loadingInfo}
                    >
                      {loadingInfo ? "Loading..." : "Enrich with IPInfo"}
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void loadRipeNetworkInfo()}
                    disabled={loadingInfo}
                  >
                    {loadingInfo ? "Loading..." : "Load Network Info"}
                  </Button>
                </div>
              </>
            ) : (
              <>
                <p className="text-muted-foreground text-sm">
                  Original IP: <Badge variant="secondary">{ip}</Badge>
                </p>
                <Card>
                  <CardContent className="flex flex-wrap items-center gap-4">
                    <div className="flex-1 space-y-1">
                      {ipInfo.org && (
                        <p className="text-sm">
                          <strong>Org:</strong> {ipInfo.org}
                        </p>
                      )}
                      {ipInfo.hostname && (
                        <p className="text-muted-foreground text-sm">
                          <strong>Hostname:</strong> {ipInfo.hostname}
                        </p>
                      )}
                      {(ipInfo.city || ipInfo.region || ipInfo.country) && (
                        <p className="text-muted-foreground text-sm">
                          <strong>Location:</strong>{" "}
                          {[ipInfo.city, ipInfo.region, ipInfo.country]
                            .filter(Boolean)
                            .join(", ")}
                        </p>
                      )}
                      {asn && loadingPrefixes && (
                        <p className="text-secondary text-sm">
                          Loading AS{asn} prefixes...
                        </p>
                      )}
                    </div>
                    {ipInfo.hostname && onAddHostname && (
                      <Button size="sm" onClick={handleAddHostname}>
                        Add Hostname
                      </Button>
                    )}
                  </CardContent>
                </Card>
              </>
            )}
          </div>

          {sets.length > 0 && (
            <SetSelector
              sets={sets}
              value={selectedSetId}
              onChange={(setId, name) => {
                setSelectedSetId(setId);
                if (name) setNewSetName(name);
              }}
            />
          )}

          {prefixes.length > 0 ? (
            <>
              <p className="text-muted-foreground text-sm">
                Loaded {prefixes.length} prefixes from AS{asn}
              </p>
              <div className="flex gap-2">
                <Badge
                  onClick={() => {
                    setAddMode("single");
                    onSelectVariant(initialVariants[0]);
                  }}
                >
                  {`Add ${ip} only`}
                </Badge>
                <Badge
                  variant="outline"
                  onClick={() => {
                    setAddMode("all");
                    onSelectVariant(prefixes);
                  }}
                >
                  {`Add all ${prefixes.length} prefixes`}
                </Badge>
              </div>
            </>
          ) : (
            <>
              <p className="text-muted-foreground text-sm">CIDR variants:</p>
              <RadioGroup
                value={selected}
                onValueChange={(value) => onSelectVariant(value)}
              >
                {variants.map((variant) => {
                  const [, cidr] = variant.split("/");
                  let description: string;
                  if (cidr === "32" || cidr === "128")
                    description = "Single IP";
                  else if (cidr === "24")
                    description = "~256 IPs - local subnet";
                  else if (cidr === "16")
                    description = "~65K IPs - network block";
                  else if (cidr === "8") description = "~16M IPs - class A";
                  else if (cidr === "64") description = "IPv6 subnet";
                  else if (cidr === "48") description = "IPv6 site";
                  else description = "IPv6 ISP range";

                  return (
                    <Label key={variant} htmlFor={`variant-${variant}`}>
                      <Field
                        orientation="horizontal"
                        className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 border-border border p-2"
                      >
                        <FieldContent>
                          <FieldTitle className="font-medium">
                            {variant}
                          </FieldTitle>
                          <FieldDescription>{description}</FieldDescription>
                        </FieldContent>
                        <RadioGroupItem
                          value={variant}
                          id={`variant-${variant}`}
                        />
                      </Field>
                    </Label>
                  );
                })}
              </RadioGroup>
            </>
          )}
        </div>
        <Separator />
        <DialogFooter>
          <Button onClick={onClose} variant="ghost">
            Cancel
          </Button>
          <div className="flex-1" />
          <Button
            onClick={handleAdd}
            disabled={!selected && prefixes.length === 0}
          >
            <AddIcon />
            {addMode === "all" && prefixes.length > 0
              ? `Add All ${prefixes.length} Prefixes`
              : "Add IP/CIDR"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

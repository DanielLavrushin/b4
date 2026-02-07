import dns from "@assets/dns.json";
import {
  BlockIcon,
  CheckIcon,
  DnsIcon,
  SecurityIcon,
  SpeedIcon,
} from "@b4.icons";
import { cn } from "@design/lib/utils";
import { B4SetConfig } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from "@primitives/field";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";
import { Switch } from "@primitives/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@primitives/table";

interface DnsEntry {
  name: string;
  ip: string;
  ipv6: boolean;
  desc: string;
  dnssec?: boolean;
  tags: string[];
  warn?: boolean;
}

interface DnsSettingsProps {
  config: B4SetConfig;
  ipv6: boolean;
  onChange: (field: string, value: string | boolean) => void;
}

const POPULAR_DNS = (dns as DnsEntry[]).sort((a, b) =>
  a.name.localeCompare(b.name),
);

export function DnsSettings({ config, onChange, ipv6 }: DnsSettingsProps) {
  const dns = config.dns || { enabled: false, target_dns: "" };
  const selectedServer = POPULAR_DNS.find((d) => d.ip === dns.target_dns);

  const handleServerSelect = (ip: string) => {
    onChange("dns.target_dns", ip);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>DNS Redirect</CardTitle>
        <CardDescription>
          Redirect DNS queries to bypass ISP DNS poisoning
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        <FieldGroup>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Enable DNS Redirect</FieldTitle>
              <FieldDescription>
                Redirect DNS queries for domains in this set to specified DNS
                server
              </FieldDescription>
            </FieldContent>
            <Switch
              id="switch-dns-enabled"
              checked={dns.enabled}
              onCheckedChange={(checked: boolean) =>
                onChange("dns.enabled", checked)
              }
            />
          </Field>

          {dns.enabled && (
            <>
              {/* Custom IP input */}
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>Fragment DNS Queries</FieldTitle>
                  <FieldDescription>
                    Split DNS packets using IP fragmentation to bypass DPI that
                    pattern-matches domain names in queries
                  </FieldDescription>
                </FieldContent>
                <Switch
                  id="switch-dns-fragment-query"
                  checked={dns.fragment_query || false}
                  onCheckedChange={(checked: boolean) =>
                    onChange("dns.fragment_query", checked)
                  }
                />
              </Field>

              <Field>
                <FieldLabel>DNS Server IP</FieldLabel>
                <Input
                  value={dns.target_dns}
                  onChange={(e) => onChange("dns.target_dns", e.target.value)}
                  placeholder="e.g., 9.9.9.9"
                />
                <FieldDescription>
                  Select below or enter custom IP
                </FieldDescription>
              </Field>

              {selectedServer && (
                <Field>
                  <FieldLabel>
                    <DnsIcon className="text-primary size-5" />
                    {selectedServer.name}
                    {selectedServer.dnssec && (
                      <Badge variant="outline">
                        <SecurityIcon />
                        DNSSEC
                      </Badge>
                    )}
                  </FieldLabel>
                  <FieldDescription>{selectedServer.desc}</FieldDescription>
                </Field>
              )}
            </>
          )}
        </FieldGroup>

        <Separator className="my-4" />

        {dns.enabled && (
          <FieldSet>
            <FieldLegend>Recommended DNS Servers</FieldLegend>
            <div className="bg-card border-border max-h-75 overflow-auto border">
              <Table>
                <TableHeader>
                  <TableRow>
                    {["", "Name", "IP", "Description"].map((label) => (
                      <TableHead key={label}>{label}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {POPULAR_DNS.filter((server) =>
                    ipv6 ? server.ipv6 : !server.ipv6,
                  ).map((server) => (
                    <TableRow
                      key={server.ip}
                      onClick={() => handleServerSelect(server.ip)}
                      data-state={
                        dns.target_dns === server.ip ? "selected" : undefined
                      }
                    >
                      <TableCell>
                        {dns.target_dns === server.ip && (
                          <CheckIcon className="text-primary size-5" />
                        )}
                        {dns.target_dns !== server.ip && server.warn && (
                          <BlockIcon className="text-destructive size-5" />
                        )}
                        {dns.target_dns !== server.ip && !server.warn && (
                          <DnsIcon className="text-muted-foreground size-5" />
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span
                            className={cn(
                              "font-mono",
                              server.warn && "text-destructive",
                            )}
                          >
                            {server.name}
                          </span>
                          {server.tags.includes("fast") && (
                            <SpeedIcon className="text-primary h-3.5 w-3.5" />
                          )}
                          {server.tags.includes("adblock") && (
                            <BlockIcon className="text-primary h-3.5 w-3.5" />
                          )}
                        </div>
                      </TableCell>
                      <TableCell>{server.ip}</TableCell>
                      <TableCell
                        className={cn(server.warn && "text-destructive")}
                      >
                        {server.desc}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </FieldSet>
        )}

        {/* Warnings */}
        {!dns.target_dns && (
          <Alert variant="destructive" className="m-0 md:col-span-2">
            <AlertDescription>
              Select or enter a DNS server IP to enable redirect.
            </AlertDescription>
          </Alert>
        )}

        {dns.target_dns === "8.8.8.8" && (
          <Alert variant="destructive" className="m-0 md:col-span-2">
            <AlertDescription>
              Google DNS (8.8.8.8) is commonly poisoned by Russian ISPs.
              Consider Quad9 (9.9.9.9) or Cloudflare (1.1.1.1) instead.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}

import dns from "@assets/dns.json";
import {
  DnsIcon,
  BlockIcon,
  CheckIcon,
  SpeedIcon,
  SecurityIcon,
} from "@b4.icons";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldTitle,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Switch } from "@design/components/ui/switch";
import { cn } from "@design/lib/utils";
import { B4SetConfig } from "@models/config";

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
  a.name.localeCompare(b.name)
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
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
            <DnsIcon />
          </div>
          <div className="flex-1">
            <CardTitle>DNS Redirect</CardTitle>
            <CardDescription className="mt-1">
              Redirect DNS queries to bypass ISP DNS poisoning
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <Alert className="m-0 md:col-span-2">
            <AlertDescription>
              Some ISPs intercept DNS queries (especially to 8.8.8.8) and return
              fake IPs for blocked domains. DNS redirect transparently rewrites
              your DNS queries to use an unpoisoned resolver.
            </AlertDescription>
          </Alert>

          <div>
            <label htmlFor="switch-dns-enabled">
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldTitle>Enable DNS Redirect</FieldTitle>
                  <FieldDescription>
                    Redirect DNS queries for domains in this set to specified
                    DNS server
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
            </label>
          </div>

          {dns.enabled && (
            <>
              {/* Custom IP input */}
              <div>
                <label htmlFor="switch-dns-fragment-query">
                  <Field orientation="horizontal">
                    <FieldContent>
                      <FieldTitle>Fragment DNS Queries</FieldTitle>
                      <FieldDescription>
                        Split DNS packets using IP fragmentation to bypass DPI
                        that pattern-matches domain names in queries
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
                </label>
              </div>
              <div>
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
              </div>

              <div>
                {selectedServer && (
                  <div className="p-4 bg-card rounded-md border border-border h-full">
                    <div className="flex items-center gap-2">
                      <DnsIcon className="h-5 w-5 text-primary" />
                      <p className="text-sm font-semibold">
                        {selectedServer.name}
                      </p>
                      {selectedServer.dnssec && (
                        <Badge
                          variant="outline"
                          className="inline-flex items-center gap-1"
                        >
                          <SecurityIcon className="h-3 w-3" />
                          DNSSEC
                        </Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground mt-2">
                      {selectedServer.desc}
                    </p>
                  </div>
                )}
              </div>

              {/* DNS server list */}
              <div className="md:col-span-2">
                <p className="text-sm font-semibold mb-2">
                  Recommended DNS Servers
                </p>
                <div className="border border-border rounded-md bg-card max-h-80 overflow-auto">
                  <div className="divide-y divide-border">
                    {POPULAR_DNS.filter((server) =>
                      ipv6 ? server.ipv6 : !server.ipv6
                    ).map((server) => (
                      <button
                        key={server.ip}
                        onClick={() => handleServerSelect(server.ip)}
                        className={cn(
                          "w-full p-3 text-left hover:bg-accent transition-colors flex items-start gap-3 border-l-3",
                          dns.target_dns === server.ip
                            ? "bg-accent border-l-secondary"
                            : server.warn
                            ? "border-l-quaternary"
                            : "border-l-transparent"
                        )}
                      >
                        <div className="min-w-9 flex items-center">
                          {dns.target_dns === server.ip ? (
                            <CheckIcon className="h-5 w-5 text-primary" />
                          ) : server.warn ? (
                            <BlockIcon className="h-5 w-5 text-destructive" />
                          ) : (
                            <DnsIcon className="h-5 w-5 text-muted-foreground" />
                          )}
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2 flex-wrap">
                            <p
                              className={cn(
                                "text-sm font-mono",
                                server.warn
                                  ? "text-destructive"
                                  : "text-foreground"
                              )}
                            >
                              {server.name}
                            </p>
                            <p className="text-sm text-muted-foreground">
                              {server.ip}
                            </p>
                            {server.tags.includes("fast") && (
                              <SpeedIcon className="h-3.5 w-3.5 text-primary" />
                            )}
                            {server.tags.includes("adblock") && (
                              <BlockIcon className="h-3.5 w-3.5 text-primary" />
                            )}
                          </div>
                          <p
                            className={cn(
                              "text-xs mt-1",
                              server.warn
                                ? "text-destructive"
                                : "text-muted-foreground"
                            )}
                          >
                            {server.desc}
                          </p>
                        </div>
                      </button>
                    ))}
                  </div>
                </div>
              </div>

              {/* Visual explanation */}
              <div className="md:col-span-2">
                <div className="p-4 bg-card rounded-md border border-border">
                  <p className="text-xs text-muted-foreground mb-2">
                    HOW IT WORKS
                  </p>
                  <div className="flex items-center gap-2 flex-wrap">
                    <Badge className="bg-accent">App</Badge>
                    <p className="text-xs text-muted-foreground">
                      → DNS query for
                    </p>
                    <Badge className="bg-accent text-accent-foreground">
                      instagram.com
                    </Badge>
                    <p className="text-xs text-muted-foreground">→</p>
                    <Badge className="bg-destructive/20 text-destructive line-through">
                      poisoned DNS
                    </Badge>
                    <p className="text-xs text-muted-foreground">→</p>
                    <Badge
                      className={cn(
                        "text-xs px-1.5 py-0.5",
                        dns.target_dns
                          ? "bg-primary text-primary-foreground"
                          : "bg-accent text-accent-foreground"
                      )}
                    >
                      {dns.target_dns || "select DNS"}
                    </Badge>
                    <p className="text-xs text-muted-foreground">→ Real IP</p>
                  </div>
                </div>
              </div>

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
            </>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

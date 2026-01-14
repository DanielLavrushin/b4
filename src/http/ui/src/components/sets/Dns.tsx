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
        <div className="flex items-center gap-3">
          <div className="bg-accent text-accent-foreground flex h-10 w-10 items-center justify-center rounded-md">
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
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
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
                  <div className="bg-card border-border h-full rounded-md border p-4">
                    <div className="flex items-center gap-2">
                      <DnsIcon className="text-primary h-5 w-5" />
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
                    <p className="text-muted-foreground mt-2 text-xs">
                      {selectedServer.desc}
                    </p>
                  </div>
                )}
              </div>

              {/* DNS server list */}
              <div className="md:col-span-2">
                <p className="mb-2 text-sm font-semibold">
                  Recommended DNS Servers
                </p>
                <div className="border-border bg-card max-h-80 overflow-auto rounded-md border">
                  <div className="divide-border divide-y">
                    {POPULAR_DNS.filter((server) =>
                      ipv6 ? server.ipv6 : !server.ipv6,
                    ).map((server) => (
                      <button
                        key={server.ip}
                        onClick={() => handleServerSelect(server.ip)}
                        className={cn(
                          "hover:bg-accent flex w-full items-start gap-3 border-l-3 p-3 text-left transition-colors",
                          dns.target_dns === server.ip
                            ? "bg-accent border-l-secondary"
                            : server.warn
                              ? "border-l-quaternary"
                              : "border-l-transparent",
                        )}
                      >
                        <div className="flex min-w-9 items-center">
                          {dns.target_dns === server.ip ? (
                            <CheckIcon className="text-primary h-5 w-5" />
                          ) : server.warn ? (
                            <BlockIcon className="text-destructive h-5 w-5" />
                          ) : (
                            <DnsIcon className="text-muted-foreground h-5 w-5" />
                          )}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <p
                              className={cn(
                                "font-mono text-sm",
                                server.warn
                                  ? "text-destructive"
                                  : "text-foreground",
                              )}
                            >
                              {server.name}
                            </p>
                            <p className="text-muted-foreground text-sm">
                              {server.ip}
                            </p>
                            {server.tags.includes("fast") && (
                              <SpeedIcon className="text-primary h-3.5 w-3.5" />
                            )}
                            {server.tags.includes("adblock") && (
                              <BlockIcon className="text-primary h-3.5 w-3.5" />
                            )}
                          </div>
                          <p
                            className={cn(
                              "mt-1 text-xs",
                              server.warn
                                ? "text-destructive"
                                : "text-muted-foreground",
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
                <div className="bg-card border-border rounded-md border p-4">
                  <p className="text-muted-foreground mb-2 text-xs">
                    HOW IT WORKS
                  </p>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge className="bg-accent">App</Badge>
                    <p className="text-muted-foreground text-xs">
                      → DNS query for
                    </p>
                    <Badge className="bg-accent text-accent-foreground">
                      instagram.com
                    </Badge>
                    <p className="text-muted-foreground text-xs">→</p>
                    <Badge className="bg-destructive/20 text-destructive line-through">
                      poisoned DNS
                    </Badge>
                    <p className="text-muted-foreground text-xs">→</p>
                    <Badge
                      className={cn(
                        "px-1.5 py-0.5 text-xs",
                        dns.target_dns
                          ? "bg-primary text-primary-foreground"
                          : "bg-accent text-accent-foreground",
                      )}
                    >
                      {dns.target_dns || "select DNS"}
                    </Badge>
                    <p className="text-muted-foreground text-xs">→ Real IP</p>
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

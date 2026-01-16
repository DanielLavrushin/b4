import { InfoIcon, IpIcon, CategoryIcon, DomainIcon } from "@b4.icons";
import { useEffect, useState } from "react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { ComboboxMultiple } from "@composed/combobox-multiple";
import { Label } from "@primitives/label";
import { TagsInput } from "@composed/tags-input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@primitives/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
import { B4SetConfig, GeoConfig } from "@models/config";
import { SetStats } from "./Manager";

interface TargetSettingsProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  onChange: (field: string, value: string | string[]) => void;
}

// Hook for loading categories from API
function useCategories(endpoint: string, enabled: boolean) {
  const [categories, setCategories] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setLoading(true);
    fetch(endpoint)
      .then((res) => res.ok && res.json())
      .then((data: { tags?: string[] }) => {
        if (!cancelled) {
          setCategories(data?.tags || []);
        }
      })
      .catch((err) => {
        if (!cancelled)
          console.error(`Failed to load categories from ${endpoint}:`, err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [endpoint, enabled]);

  return { categories, loading };
}

export const TargetSettings = ({
  config,
  onChange,
  geo,
  stats,
}: TargetSettingsProps) => {
  const [tabValue, setTabValue] = useState(0);

  const { categories: availableCategories, loading: loadingCategories } =
    useCategories("/api/geosite", !!geo.sitedat_path);
  const {
    categories: availableGeoIPCategories,
    loading: loadingGeoIPCategories,
  } = useCategories("/api/geoip", !!geo.ipdat_path);

  return (
    <>
      <div className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <div className="flex items-center gap-3">
              <div className="bg-accent text-accent-foreground flex size-10 items-center justify-center rounded-md">
                <DomainIcon />
              </div>
              <div className="flex-1">
                <CardTitle>Domain Filtering Configuration</CardTitle>
                <CardDescription className="mt-1">
                  Configure domain matching for DPI bypass and blocking
                </CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            <div className="border-border mb-0 border-b">
              <Tabs
                value={tabValue.toString()}
                onValueChange={(v) => setTabValue(Number(v))}
                className="w-full"
              >
                <TabsList
                  variant="line"
                  className="border-border h-auto rounded-none border-b bg-transparent p-0"
                >
                  <TabsTrigger
                    value="0"
                    className="data-[state=active]:border-primary rounded-none border-b-2 border-transparent data-[state=active]:border-b-2"
                  >
                    <div className="flex items-center gap-1.5">
                      <DomainIcon />
                      <span>Bypass Domains</span>
                    </div>
                  </TabsTrigger>
                  <TabsTrigger
                    value="1"
                    className="data-[state=active]:border-primary rounded-none border-b-2 border-transparent data-[state=active]:border-b-2"
                  >
                    <div className="flex items-center gap-1.5">
                      <IpIcon />
                      <span>Bypass IPs</span>
                    </div>
                  </TabsTrigger>
                </TabsList>

                {/* DPI Bypass Tab */}
                <TabsContent value="0" className="pt-6">
                  {/* Manual Bypass Domains */}
                  <div className="flex flex-col gap-1.5">
                    <Label className="text-sm font-medium">
                      <DomainIcon /> Manual Bypass Domains
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <InfoIcon className="text-muted-foreground size-4" />
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Add specific domains to bypass DPI.</p>
                        </TooltipContent>
                      </Tooltip>
                    </Label>
                    <TagsInput
                      value={config.targets.sni_domains}
                      onValueChange={(values) =>
                        onChange("targets.sni_domains", values)
                      }
                      placeholder="example.com"
                    />
                  </div>

                  {/* GeoSite Categories */}
                  {geo.sitedat_path && (
                    <div className="mt-4 flex flex-col gap-1.5">
                      <Label className="text-sm font-medium">
                        <CategoryIcon /> Bypass GeoSite Categories
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <InfoIcon className="text-muted-foreground size-4" />
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>
                              Load predefined domain lists from GeoSite database
                              for DPI bypass
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </Label>
                      <ComboboxMultiple
                        items={availableCategories}
                        value={config.targets.geosite_categories}
                        onValueChange={(values) =>
                          onChange("targets.geosite_categories", values)
                        }
                        placeholder={
                          loadingCategories
                            ? "Loading..."
                            : "Search categories..."
                        }
                        emptyMessage="No categories found."
                        disabled={loadingCategories}
                        breakdown={stats?.geosite_category_breakdown}
                      />
                    </div>
                  )}
                </TabsContent>

                {/* Bypass IPs Tab */}
                <TabsContent value="1" className="pt-6">
                  {/* Manual Bypass IPs */}
                  <div className="flex flex-col gap-1.5">
                    <Label className="text-sm font-medium">
                      <IpIcon /> Manual Bypass IPs
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <InfoIcon className="text-muted-foreground size-4" />
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Add specific ip/cidr to bypass DPI.</p>
                        </TooltipContent>
                      </Tooltip>
                    </Label>
                    <TagsInput
                      value={config.targets.ip}
                      onValueChange={(values) => onChange("targets.ip", values)}
                      placeholder="192.168.1.1"
                    />
                  </div>

                  {/* GeoIP Categories */}
                  {geo.ipdat_path && (
                    <div className="mt-4 flex flex-col gap-1.5">
                      <Label className="text-sm font-medium">
                        <CategoryIcon /> Bypass GeoIP Categories
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <InfoIcon className="text-muted-foreground size-4" />
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>
                              Load predefined IP lists from GeoIP database for
                              DPI bypass
                            </p>
                          </TooltipContent>
                        </Tooltip>
                      </Label>
                      <ComboboxMultiple
                        items={availableGeoIPCategories}
                        value={config.targets.geoip_categories}
                        onValueChange={(values) =>
                          onChange("targets.geoip_categories", values)
                        }
                        placeholder={
                          loadingGeoIPCategories
                            ? "Loading..."
                            : "Search categories..."
                        }
                        emptyMessage="No categories found."
                        disabled={loadingGeoIPCategories}
                        breakdown={stats?.geoip_category_breakdown}
                      />
                    </div>
                  )}
                </TabsContent>
              </Tabs>
            </div>
          </CardContent>
        </Card>
      </div>
    </>
  );
};

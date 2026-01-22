import { DomainIcon, IpIcon } from "@b4.icons";
import { useEffect, useState } from "react";

import { ComboboxMultiple } from "@composed/combobox-multiple";
import { TagsInput } from "@composed/tags-input";
import {
  Field,
  FieldContent,
  FieldLabel,
  FieldSet,
} from "@design/primitives/field";
import { Separator } from "@design/primitives/separator";
import { B4SetConfig, GeoConfig } from "@models/config";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@primitives/tabs";
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
    <Card>
      <CardHeader>
        <CardTitle>Domain Filtering Configuration</CardTitle>
        <CardDescription>
          Configure domain matching for DPI bypass and blocking
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Tabs
          value={tabValue.toString()}
          onValueChange={(v) => setTabValue(Number(v))}
          className="w-full"
        >
          <TabsList variant="line" className="p-0">
            <TabsTrigger value="0">
              <DomainIcon />
              Bypass Domains
            </TabsTrigger>
            <TabsTrigger value="1">
              <IpIcon />
              Bypass IPs
            </TabsTrigger>
          </TabsList>

          <Separator className="my-4" />

          {/* DPI Bypass Tab */}
          <TabsContent value="0">
            <FieldSet>
              <Field>
                <FieldContent>
                  <FieldLabel>Manual Bypass Domains</FieldLabel>
                </FieldContent>
                <TagsInput
                  value={config.targets.sni_domains}
                  onValueChange={(values) =>
                    onChange("targets.sni_domains", values)
                  }
                  placeholder="example.com"
                />
              </Field>

              {/* GeoSite Categories */}
              {geo.sitedat_path && (
                <Field>
                  <FieldContent>
                    <FieldLabel>Bypass GeoSite Categories</FieldLabel>
                  </FieldContent>
                  <ComboboxMultiple
                    items={availableCategories}
                    value={config.targets.geosite_categories}
                    onValueChange={(values) =>
                      onChange("targets.geosite_categories", values)
                    }
                    placeholder={
                      loadingCategories ? "Loading..." : "Search categories..."
                    }
                    emptyMessage="No categories found."
                    loading={loadingCategories}
                    breakdown={stats?.geosite_category_breakdown}
                  />
                </Field>
              )}
            </FieldSet>
          </TabsContent>

          {/* Bypass IPs Tab */}
          <TabsContent value="1">
            <FieldSet>
              <Field>
                <FieldContent>
                  <FieldLabel>Manual Bypass IPs</FieldLabel>
                </FieldContent>
                <TagsInput
                  value={config.targets.ip}
                  onValueChange={(values) => onChange("targets.ip", values)}
                  placeholder="192.168.1.1"
                />
              </Field>

              {/* GeoIP Categories */}
              {geo.ipdat_path && (
                <Field>
                  <FieldContent>
                    <FieldLabel>Bypass GeoIP Categories</FieldLabel>
                  </FieldContent>
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
                    loading={loadingGeoIPCategories}
                    breakdown={stats?.geoip_category_breakdown}
                  />
                </Field>
              )}
            </FieldSet>
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  );
};

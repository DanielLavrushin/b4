import {
  InfoIcon,
  IconLoader2,
  IpIcon,
  AddIcon,
  CategoryIcon,
  ClearIcon,
  DomainIcon,
} from "@b4.icons";
import * as React from "react";
import { useDeferredValue, useEffect, useState, useTransition } from "react";

import { ChipList } from "@components/common/ChipList";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  useComboboxAnchor,
} from "@design/components/ui/combobox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Field, FieldLabel } from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import { Separator } from "@design/components/ui/separator";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@design/components/ui/tabs";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { B4SetConfig, GeoConfig } from "@models/config";
import { SetStats } from "./Manager";

interface TargetSettingsProps {
  config: B4SetConfig;
  geo: GeoConfig;
  stats?: SetStats;
  onChange: (field: string, value: string | string[]) => void;
}

interface CategoryPreview {
  category: string;
  total_domains: number;
  preview_count: number;
  preview: string[];
}

interface CategoryListProps {
  value: string[];
  options: string[];
  onValueChange: (values: string[]) => void;
  loading?: boolean;
  searchPlaceholder?: string;
  helperText?: string;
  onCategoryClick?: (category: string) => void;
  getCategoryLabel?: (category: string) => React.ReactNode;
}

function CategoryList({
  value,
  options,
  onValueChange,
  loading = false,
  searchPlaceholder = "Search categories...",
  helperText,
  onCategoryClick,
  getCategoryLabel,
}: CategoryListProps) {
  const [inputValue, setInputValue] = useState("");
  const anchor = useComboboxAnchor();
  const [isPending, startTransition] = useTransition();

  // Use deferred value to keep input responsive while filtering happens in background
  const deferredInputValue = useDeferredValue(inputValue);

  // Optimize value lookup with Set
  const valueSet = React.useMemo(() => new Set(value), [value]);

  // Simple fuzzy search function - matches if all characters appear in order
  const fuzzyMatch = React.useCallback(
    (text: string, pattern: string): boolean => {
      const textLower = text.toLowerCase();
      const patternLower = pattern.toLowerCase();
      let patternIndex = 0;
      for (
        let i = 0;
        i < textLower.length && patternIndex < patternLower.length;
        i++
      ) {
        if (textLower[i] === patternLower[patternIndex]) {
          patternIndex++;
        }
      }
      return patternIndex === patternLower.length;
    },
    []
  );

  // Filter options using fuzzy search with deferred value for better performance
  // Limit results to prevent lag when there are many matches
  const MAX_DISPLAYED_RESULTS = 100;
  const filteredOptions = React.useMemo(() => {
    const searchTerm = (deferredInputValue || "").trim().toLowerCase();

    if (!searchTerm) {
      // When no search term, show all options but limit to prevent initial lag
      return options.slice(0, MAX_DISPLAYED_RESULTS);
    }

    // Use fuzzy search for better matching
    // First try exact matches (starts with), then fuzzy matches
    const exactMatches: string[] = [];
    const fuzzyMatches: string[] = [];

    for (const opt of options) {
      const optLower = opt.toLowerCase();
      if (optLower.startsWith(searchTerm)) {
        exactMatches.push(opt);
      } else if (fuzzyMatch(opt, searchTerm)) {
        fuzzyMatches.push(opt);
      }
      // Stop if we have enough results
      if (exactMatches.length + fuzzyMatches.length >= MAX_DISPLAYED_RESULTS) {
        break;
      }
    }

    return [...exactMatches, ...fuzzyMatches].slice(0, MAX_DISPLAYED_RESULTS);
  }, [options, deferredInputValue, fuzzyMatch]);

  // Memoize handleToggle to avoid recreating on every render
  const handleToggleMemo = React.useCallback(
    (category: string) => {
      if (valueSet.has(category)) {
        onValueChange(value.filter((c) => c !== category));
      } else {
        onValueChange([...value, category]);
      }
      setInputValue("");
    },
    [valueSet, value, onValueChange]
  );

  // Memoize rendered items to avoid re-rendering on every state change
  // Only recompute when filteredOptions actually changes (not on every inputValue change)
  const renderedItems = React.useMemo(() => {
    return filteredOptions.map((option) => {
      const isSelected = valueSet.has(option);
      return (
        <ComboboxItem
          key={option}
          value={option}
          onClick={() => handleToggleMemo(option)}
        >
          <span
            className="flex-1 text-sm cursor-pointer flex items-center gap-2"
            onClick={(e) => {
              e.stopPropagation();
              onCategoryClick?.(option);
            }}
          >
            {getCategoryLabel ? getCategoryLabel(option) : option}
          </span>
        </ComboboxItem>
      );
    });
  }, [
    filteredOptions,
    valueSet,
    handleToggleMemo,
    onCategoryClick,
    getCategoryLabel,
  ]);

  return (
    <div className="flex flex-col gap-2">
      <Combobox autoHighlight>
        <div ref={anchor}>
          <ComboboxInput
            placeholder={searchPlaceholder}
            className="w-full"
            showClear={!!inputValue}
            showTrigger={false}
            value={inputValue}
            onChange={(e) => {
              const newValue = e.target.value;
              // Update input value in a transition to prioritize input responsiveness
              // This allows React to batch updates and prioritize the input field
              startTransition(() => {
                setInputValue(newValue);
              });
            }}
          />
        </div>
        <ComboboxContent anchor={anchor} className="max-h-75">
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <IconLoader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <ComboboxList>
              {filteredOptions.length === 0 && (
                <ComboboxEmpty>No categories found</ComboboxEmpty>
              )}
              {renderedItems}
            </ComboboxList>
          )}
        </ComboboxContent>
      </Combobox>
      {helperText && (
        <p className="text-xs text-muted-foreground">{helperText}</p>
      )}
    </div>
  );
}

// Hook for loading categories
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

// Generic function to add items to a list
function useListManager<T extends string>(
  currentList: T[],
  onChange: (field: string, value: T[]) => void,
  field: string
) {
  const addItems = React.useCallback(
    (input: string, setInput: (v: string) => void) => {
      const trimmed = input.trim();
      if (!trimmed) return;

      const items = trimmed
        .split(/[\s,|]+/)
        .filter(Boolean)
        .map((s) => s.trim());
      const existing = new Set(currentList);
      const newItems = items.filter((item) => item && !existing.has(item as T));

      if (newItems.length > 0) {
        onChange(field, [...currentList, ...(newItems as T[])]);
      }
      setInput("");
    },
    [currentList, onChange, field]
  );

  const removeItem = React.useCallback(
    (item: T) => {
      onChange(
        field,
        currentList.filter((i) => i !== item)
      );
    },
    [currentList, onChange, field]
  );

  const clearAll = React.useCallback(() => {
    onChange(field, []);
  }, [onChange, field]);

  return { addItems, removeItem, clearAll };
}

export const TargetSettings = ({
  config,
  onChange,
  geo,
  stats,
}: TargetSettingsProps) => {
  const [tabValue, setTabValue] = useState(0);
  const [newBypassDomain, setNewBypassDomain] = useState("");
  const [newBypassIP, setNewBypassIP] = useState("");

  const { categories: availableCategories, loading: loadingCategories } =
    useCategories("/api/geosite", !!geo.sitedat_path);
  const {
    categories: availableGeoIPCategories,
    loading: loadingGeoIPCategories,
  } = useCategories("/api/geoip", !!geo.ipdat_path);

  const [previewDialog, setPreviewDialog] = useState<{
    open: boolean;
    category: string;
    data?: CategoryPreview;
    loading: boolean;
  }>({ open: false, category: "", loading: false });

  const domainsManager = useListManager(
    config.targets.sni_domains,
    onChange,
    "targets.sni_domains"
  );
  const ipsManager = useListManager(config.targets.ip, onChange, "targets.ip");

  const previewCategory = React.useCallback(async (category: string) => {
    setPreviewDialog({ open: true, category, loading: true });
    try {
      const response = await fetch(
        `/api/geosite/category?tag=${encodeURIComponent(category)}`
      );
      if (response.ok) {
        const data = (await response.json()) as CategoryPreview;
        setPreviewDialog((prev) => ({ ...prev, data, loading: false }));
      } else {
        setPreviewDialog((prev) => ({ ...prev, loading: false }));
      }
    } catch (error) {
      console.error("Failed to preview category:", error);
      setPreviewDialog((prev) => ({ ...prev, loading: false }));
    }
  }, []);

  const renderCategoryLabel = React.useCallback(
    (category: string, breakdown?: Record<string, number>) => {
      const count = breakdown?.[category];
      return (
        <div className="flex items-center gap-1">
          <span>{category}</span>
          {count && (
            <span className="bg-accent px-1 rounded font-semibold text-xs">
              {count}
            </span>
          )}
        </div>
      );
    },
    []
  );

  return (
    <>
      <div className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
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
            <div className="border-b border-border mb-0">
              <Tabs
                value={tabValue.toString()}
                onValueChange={(v) => setTabValue(Number(v))}
                className="w-full"
              >
                <TabsList
                  variant="line"
                  className="border-b border-border rounded-none bg-transparent p-0 h-auto"
                >
                  <TabsTrigger
                    value="0"
                    className="data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none border-b-2 border-transparent"
                  >
                    <div className="flex items-center gap-1.5">
                      <DomainIcon />
                      <span>Bypass Domains</span>
                    </div>
                  </TabsTrigger>
                  <TabsTrigger
                    value="1"
                    className="data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none border-b-2 border-transparent"
                  >
                    <div className="flex items-center gap-1.5">
                      <IpIcon />
                      <span>Bypass IPs</span>
                    </div>
                  </TabsTrigger>
                </TabsList>

                {/* DPI Bypass Tab */}
                <TabsContent value="0" className="pt-6">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {/* Manual Bypass Domains */}
                    <div>
                      <div className="flex flex-col gap-1.5">
                        <Label className="text-sm font-medium">
                          <DomainIcon className="h-5 w-5" /> Manual Bypass
                          Domains
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <InfoIcon className="h-4 w-4 text-muted-foreground" />
                            </TooltipTrigger>
                            <TooltipContent>
                              <p>Add specific domains to bypass DPI.</p>
                            </TooltipContent>
                          </Tooltip>
                        </Label>
                        <div className="flex gap-2 items-start">
                          <Field className="flex-1">
                            <Input
                              value={newBypassDomain}
                              onChange={(e) =>
                                setNewBypassDomain(e.target.value)
                              }
                              onKeyDown={(e) => {
                                if (
                                  e.key === "Enter" ||
                                  e.key === "Tab" ||
                                  e.key === ","
                                ) {
                                  e.preventDefault();
                                  domainsManager.addItems(
                                    newBypassDomain,
                                    setNewBypassDomain
                                  );
                                }
                              }}
                              placeholder="example.com"
                            />
                          </Field>
                          <Button
                            variant="secondary"
                            size="icon"
                            onClick={() =>
                              domainsManager.addItems(
                                newBypassDomain,
                                setNewBypassDomain
                              )
                            }
                            disabled={!newBypassDomain.trim()}
                          >
                            <AddIcon className="h-4 w-4" />
                          </Button>
                        </div>
                      </div>
                    </div>

                    {/* GeoSite Categories */}
                    {geo.sitedat_path && (
                      <div>
                        <div className="flex flex-col gap-1.5">
                          <Label className="text-sm font-medium">
                            <CategoryIcon className="h-5 w-5" /> Bypass GeoSite
                            Categories
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <InfoIcon className="h-4 w-4 text-muted-foreground" />
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>
                                  Load predefined domain lists from GeoSite
                                  database for DPI bypass
                                </p>
                              </TooltipContent>
                            </Tooltip>
                          </Label>
                          <CategoryList
                            value={config.targets.geosite_categories}
                            options={availableCategories}
                            onValueChange={(values) =>
                              onChange("targets.geosite_categories", values)
                            }
                            loading={loadingCategories}
                            searchPlaceholder="Search categories..."
                            helperText={
                              availableCategories.length > 0
                                ? `${availableCategories.length} categories available`
                                : "Loading categories..."
                            }
                            onCategoryClick={(c) => void previewCategory(c)}
                            getCategoryLabel={(c) =>
                              renderCategoryLabel(
                                c,
                                stats?.geosite_category_breakdown
                              )
                            }
                          />
                        </div>
                      </div>
                    )}
                  </div>
                  {/* ChipLists Grid */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-4">
                    <div>
                      <ChipList
                        items={config.targets.sni_domains}
                        getKey={(d) => d}
                        getLabel={(d) => d}
                        onDelete={domainsManager.removeItem}
                        emptyMessage="No bypass domains added"
                        showEmpty
                      />
                    </div>
                    {geo.sitedat_path && (
                      <div>
                        {config.targets.geosite_categories.length > 0 && (
                          <ChipList
                            items={config.targets.geosite_categories}
                            getKey={(c) => c}
                            getLabel={(c) =>
                              renderCategoryLabel(
                                c,
                                stats?.geosite_category_breakdown
                              )
                            }
                            onDelete={(c) =>
                              onChange(
                                "targets.geosite_categories",
                                config.targets.geosite_categories.filter(
                                  (cat) => cat !== c
                                )
                              )
                            }
                            onClick={(c) => void previewCategory(c)}
                          />
                        )}
                      </div>
                    )}
                  </div>
                </TabsContent>

                {/* Bypass IPs Tab */}
                <TabsContent value="1" className="pt-6">
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                    {/* Manual Bypass IPs */}
                    <div>
                      <div className="flex flex-col gap-1.5">
                        <Label className="text-sm font-medium">
                          <DomainIcon className="h-5 w-5" /> Manual Bypass IPs
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <InfoIcon className="h-4 w-4 text-muted-foreground" />
                            </TooltipTrigger>
                            <TooltipContent>
                              <p>Add specific ip/cidr to bypass DPI.</p>
                            </TooltipContent>
                          </Tooltip>
                        </Label>
                        <div className="flex gap-2 items-start">
                          <Field className="flex-1">
                            <Input
                              value={newBypassIP}
                              onChange={(e) => setNewBypassIP(e.target.value)}
                              onKeyDown={(e) => {
                                if (
                                  e.key === "Enter" ||
                                  e.key === "Tab" ||
                                  e.key === ","
                                ) {
                                  e.preventDefault();
                                  ipsManager.addItems(
                                    newBypassIP,
                                    setNewBypassIP
                                  );
                                }
                              }}
                              placeholder="192.168.1.1"
                            />
                          </Field>
                          <Button
                            variant="secondary"
                            size="icon"
                            onClick={() =>
                              ipsManager.addItems(newBypassIP, setNewBypassIP)
                            }
                            disabled={!newBypassIP.trim()}
                          >
                            <AddIcon className="h-4 w-4" />
                          </Button>
                        </div>
                      </div>
                    </div>

                    {/* GeoIP Categories */}
                    {geo.ipdat_path && availableGeoIPCategories.length > 0 && (
                      <div>
                        <div className="flex flex-col gap-1.5">
                          <Label className="text-sm font-medium">
                            <CategoryIcon className="h-5 w-5" /> Bypass GeoIP
                            Categories
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <InfoIcon className="h-4 w-4 text-muted-foreground" />
                              </TooltipTrigger>
                              <TooltipContent>
                                <p>
                                  Load predefined IP lists from GeoIP database
                                  for DPI bypass
                                </p>
                              </TooltipContent>
                            </Tooltip>
                          </Label>
                          <CategoryList
                            value={config.targets.geoip_categories}
                            options={availableGeoIPCategories}
                            onValueChange={(values: string[]) =>
                              onChange("targets.geoip_categories", values)
                            }
                            loading={loadingGeoIPCategories}
                            searchPlaceholder="Search GeoIP categories..."
                            helperText={`${availableGeoIPCategories.length} GeoIP categories available`}
                            getCategoryLabel={(c: string) =>
                              renderCategoryLabel(
                                c,
                                stats?.geoip_category_breakdown
                              )
                            }
                          />
                        </div>
                      </div>
                    )}
                  </div>
                  {/* ChipLists Grid */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4 my-4">
                    <div>
                      <ChipList
                        items={config.targets.ip}
                        getKey={(ip) => ip}
                        getLabel={(ip) => ip}
                        onDelete={ipsManager.removeItem}
                        emptyMessage="No bypass ip added"
                        showEmpty
                        maxHeight={200}
                      />
                    </div>
                    {geo.ipdat_path && availableGeoIPCategories.length > 0 && (
                      <div>
                        {config.targets.geoip_categories.length > 0 && (
                          <ChipList
                            items={config.targets.geoip_categories}
                            getKey={(c) => c}
                            getLabel={(c) =>
                              renderCategoryLabel(
                                c,
                                stats?.geoip_category_breakdown
                              )
                            }
                            onDelete={(c) =>
                              onChange(
                                "targets.geoip_categories",
                                config.targets.geoip_categories.filter(
                                  (cat) => cat !== c
                                )
                              )
                            }
                          />
                        )}
                      </div>
                    )}
                  </div>
                </TabsContent>
              </Tabs>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Preview Dialog */}
      <Dialog
        open={previewDialog.open}
        onOpenChange={(open) =>
          !open &&
          setPreviewDialog({ open: false, category: "", loading: false })
        }
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
                <CategoryIcon />
              </div>
              <div className="flex-1">
                <DialogTitle>
                  {previewDialog.category.toUpperCase()}
                </DialogTitle>
                <DialogDescription className="mt-1">
                  Category Preview
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <div className="py-4">
            {previewDialog.loading ? (
              <div className="p-4 space-y-2">
                <div className="h-4 bg-muted rounded animate-pulse" />
                <div className="h-4 bg-muted rounded animate-pulse" />
                <div className="h-4 bg-muted rounded animate-pulse" />
              </div>
            ) : previewDialog.data ? (
              <>
                <Alert className="mb-4">
                  <AlertDescription>
                    Total domains in category:{" "}
                    {previewDialog.data.total_domains}
                    {previewDialog.data.total_domains >
                      previewDialog.data.preview_count &&
                      ` (showing first ${previewDialog.data.preview_count})`}
                  </AlertDescription>
                </Alert>
                <div className="max-h-150 overflow-auto space-y-1">
                  {previewDialog.data.preview.map((domain) => (
                    <div key={domain} className="p-2 hover:bg-accent rounded">
                      <p className="text-sm">{domain}</p>
                    </div>
                  ))}
                </div>
              </>
            ) : (
              <Alert variant="destructive">
                <AlertDescription>
                  Failed to load category preview
                </AlertDescription>
              </Alert>
            )}
          </div>
          <Separator />
          <DialogFooter>
            <Button
              onClick={() =>
                setPreviewDialog({ open: false, category: "", loading: false })
              }
            >
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};

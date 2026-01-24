import { useCaptures } from "@b4.capture";
import {
  DiscoveryPhase,
  DomainPresetResult,
  StrategyFamily,
  useDiscovery,
} from "@b4.discovery";
import {
  AddIcon,
  CollapseIcon,
  DiscoveryIcon,
  ExpandIcon,
  ImprovementIcon,
  RefreshIcon,
  SpeedIcon,
  StartIcon,
  StopIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { cn } from "@design/lib/utils";
import { useSets } from "@hooks/useSets";
import { B4SetConfig } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@primitives/collapsible";
import { Field, FieldDescription, FieldLabel } from "@primitives/field";
import { Input } from "@primitives/input";
import { Progress } from "@primitives/progress";
import { Separator } from "@primitives/separator";
import { Spinner } from "@primitives/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";
import { useCallback, useEffect, useState } from "react";
import { DiscoveryAddDialog } from "./AddDialog";
import { DiscoveryLogPanel } from "./LogPanel";
import { DiscoveryOptions, DiscoveryOptionsPanel } from "./Options";

const familyNames: Record<StrategyFamily, string> = {
  none: "Baseline",
  tcp_frag: "TCP Fragmentation",
  tls_record: "TLS Record Split",
  oob: "Out-of-Band",
  ip_frag: "IP Fragmentation",
  fake_sni: "Fake SNI",
  sack: "SACK Drop",
  syn_fake: "SYN Fake",
  desync: "Desync",
  delay: "Delay",
  disorder: "Disorder",
  extsplit: "Extension Split",
  firstbyte: "First-Byte",
  combo: "Combo",
  hybrid: "Hybrid",
  window: "Window Manipulation",
  mutation: "Mutation",
  incoming: "Incoming",
};

const phaseNames: Record<DiscoveryPhase, string> = {
  baseline: "Baseline Test",
  strategy_detection: "Strategy Detection",
  optimization: "Optimization",
  combination: "Combination Test",
  dns_detection: "DNS Detection",
};

export const DiscoveryRunner = () => {
  const {
    startDiscovery,
    cancelDiscovery,
    resetDiscovery,
    addPresetAsSet,
    discoveryRunning: running,
    suiteId,
    suite,
    error,
  } = useDiscovery();
  const { showSuccess, showError } = useSnackbar();

  const { addDomainToSet } = useSets();

  const [expandedDomains, setExpandedDomains] = useState<Set<string>>(
    new Set(),
  );

  const { captures, loadCaptures } = useCaptures();
  const [options, setOptions] = useState<DiscoveryOptions>(() => ({
    skipDNS: localStorage.getItem("b4_discovery_skipdns") === "true",
    payloadFiles: [],
    validationTries:
      parseInt(localStorage.getItem("b4_discovery_validation_tries") || "1") ||
      1,
  }));

  useEffect(() => {
    void loadCaptures();
  }, [loadCaptures]);

  useEffect(() => {
    localStorage.setItem("b4_discovery_skipdns", String(options.skipDNS));
  }, [options.skipDNS]);

  useEffect(() => {
    localStorage.setItem(
      "b4_discovery_validation_tries",
      String(options.validationTries),
    );
  }, [options.validationTries]);
  const [checkUrl, setCheckUrl] = useState("");

  const [addingPreset, setAddingPreset] = useState(false);
  const [addDialog, setAddDialog] = useState<{
    open: boolean;
    domain: string;
    presetName: string;
    setConfig: B4SetConfig | null;
  }>({ open: false, domain: "", presetName: "", setConfig: null });

  const progress = suite
    ? Math.min((suite.completed_checks / suite.total_checks) * 100, 100)
    : 0;
  const isReconnecting = suiteId && running && !suite;

  const handleAddStrategy = (domain: string, result: DomainPresetResult) => {
    setAddDialog({
      open: true,
      domain,
      presetName: result.preset_name,
      setConfig: result.set || null,
    });
  };
  const toggleDomainExpand = (domain: string) => {
    setExpandedDomains((prev) => {
      const next = new Set(prev);
      if (next.has(domain)) {
        next.delete(domain);
      } else {
        next.add(domain);
      }
      return next;
    });
  };

  const handleDomainKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key !== "Enter") return;
      if (!checkUrl.trim()) return;
      e.preventDefault();
      void startDiscovery(
        checkUrl,
        options.skipDNS,
        options.payloadFiles,
        options.validationTries,
      );
    },
    [checkUrl, options.skipDNS, options.payloadFiles, startDiscovery],
  );

  const handleAddNew = async (name: string, domain: string) => {
    if (!addDialog.setConfig) return;
    setAddingPreset(true);
    const configToAdd = {
      ...addDialog.setConfig,
      name,
      targets: { ...addDialog.setConfig.targets, sni_domains: [domain] },
    };
    const res = await addPresetAsSet(configToAdd);
    if (res.success) {
      showSuccess(`Created set "${name}"`);
      setAddDialog({
        open: false,
        domain: "",
        presetName: "",
        setConfig: null,
      });
    } else {
      showError("Failed to create set");
    }
    setAddingPreset(false);
  };

  const handleAddToExisting = async (setId: string, domain: string) => {
    setAddingPreset(true);
    const res = await addDomainToSet(setId, domain);
    if (res.success) {
      showSuccess(`Added domain to set`);
      setAddDialog({
        open: false,
        domain: "",
        presetName: "",
        setConfig: null,
      });
    } else {
      showError("Failed to add domain to set");
    }
    setAddingPreset(false);
  };

  const handleReset = useCallback(() => {
    resetDiscovery();
    setExpandedDomains(new Set());
  }, [resetDiscovery]);

  const groupResultsByPhase = (results: Record<string, DomainPresetResult>) => {
    const grouped: Record<DiscoveryPhase, DomainPresetResult[]> = {
      baseline: [],
      strategy_detection: [],
      optimization: [],
      combination: [],
      dns_detection: [],
    };

    Object.values(results).forEach((result) => {
      const phase = result.phase || "strategy_detection";
      grouped[phase].push(result);
    });

    return grouped;
  };

  return (
    <div className="space-y-6">
      {/* Control Panel */}
      <Card className="flex flex-col">
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="bg-accent text-accent-foreground flex size-10 items-center justify-center rounded-md">
              <DiscoveryIcon />
            </div>
            <div className="flex-1">
              <CardTitle>Configuration Discovery</CardTitle>
              <CardDescription className="mt-1">
                Hierarchical testing: Strategy Detection → Optimization →
                Combination
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <Separator className="mb-4" />
        <CardContent className="flex flex-col gap-4">
          <Alert>
            <DiscoveryIcon className="h-3.5 w-3.5" />
            <AlertDescription>
              <strong>Configuration Discovery:</strong> Automatically test
              multiple configuration presets to find the most effective DPI
              bypass settings for the domains you specify below. B4 will
              temporarily apply different configurations and measure their
              performance.
            </AlertDescription>
          </Alert>
          {/* Header with actions */}
          <div className="flex items-end gap-4">
            <Field className="flex-1">
              <FieldLabel htmlFor="discovery-url-input">
                Domain or URL to test
              </FieldLabel>
              <div className="flex items-center gap-4">
                <div className="flex-1">
                  <Input
                    id="discovery-url-input"
                    value={checkUrl}
                    onChange={(e) => setCheckUrl(e.target.value)}
                    onKeyDown={handleDomainKeyDown}
                    placeholder="youtube.com or https://youtube.com/some/path"
                    disabled={running || !!isReconnecting}
                  />
                </div>
                {!running && !suite && (
                  <Button
                    onClick={() => {
                      void startDiscovery(
                        checkUrl,
                        options.skipDNS,
                        options.payloadFiles,
                      );
                    }}
                    disabled={!checkUrl.trim()}
                    className="whitespace-nowrap"
                  >
                    <StartIcon className="mr-2 size-4" />
                    Start Discovery
                  </Button>
                )}
                {(running || isReconnecting) && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      void cancelDiscovery();
                    }}
                    className="whitespace-nowrap"
                  >
                    <StopIcon className="mr-2 size-4" />
                    Cancel
                  </Button>
                )}
                {suite && !running && (
                  <Button
                    variant="outline"
                    onClick={handleReset}
                    className="whitespace-nowrap"
                  >
                    <RefreshIcon className="mr-2 size-4" />
                    New Discovery
                  </Button>
                )}
              </div>
              <FieldDescription>
                Enter a domain or full URL to discover optimal bypass
                configuration
              </FieldDescription>
            </Field>
          </div>
          <DiscoveryOptionsPanel
            options={options}
            onChange={setOptions}
            captures={captures}
            disabled={running || !!isReconnecting}
          />
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {isReconnecting && (
            <div className="flex items-center gap-4">
              <Spinner />
              <p className="text-muted-foreground text-sm">
                Reconnecting to running discovery...
              </p>
            </div>
          )}
          {/* Progress indicator */}
          {running && suite && (
            <div>
              <div className="mb-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <p className="text-muted-foreground text-sm">
                    {suite.current_phase && (
                      <Badge
                        variant="secondary"
                        className="mr-2 inline-flex items-center gap-1"
                      >
                        {phaseNames[suite.current_phase]}
                      </Badge>
                    )}
                    {suite.current_phase === "dns_detection"
                      ? "Checking DNS..."
                      : `${suite.completed_checks} of ${suite.total_checks} checks`}
                  </p>
                </div>
                {suite.current_phase !== "dns_detection" && (
                  <p className="text-muted-foreground text-sm">
                    {isNaN(progress) ? "0" : progress.toFixed(0)}%
                  </p>
                )}
              </div>
              <Progress
                value={
                  suite.current_phase === "dns_detection" ? null : progress
                }
                className="h-2"
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Discovery Log Panel */}
      <DiscoveryLogPanel running={running} />

      {suite?.domain_discovery_results &&
        Object.keys(suite.domain_discovery_results).length > 0 && (
          <div className="space-y-4">
            {Object.values(suite.domain_discovery_results)
              .sort((a, b) => b.best_speed - a.best_speed)
              .map((domainResult) => {
                const isExpanded = expandedDomains.has(domainResult.domain);
                const groupedResults = groupResultsByPhase(
                  domainResult.results,
                );
                const successCount = Object.values(domainResult.results).filter(
                  (r) => r.status === "complete",
                ).length;
                const totalCount = Object.keys(domainResult.results).length;

                return (
                  <Card
                    key={domainResult.domain}
                    className="border-border gap-0 overflow-hidden rounded-none border py-0"
                  >
                    {/* Domain Header */}
                    <Collapsible
                      open={isExpanded}
                      onOpenChange={() =>
                        toggleDomainExpand(domainResult.domain)
                      }
                    >
                      <CollapsibleTrigger>
                        <div className="bg-accent flex cursor-pointer items-center justify-between p-4">
                          <div className="flex items-center gap-4">
                            <Button
                              size="sm"
                              variant="ghost"
                              className="size-6 p-0"
                            >
                              {isExpanded ? <CollapseIcon /> : <ExpandIcon />}
                            </Button>
                            <h6 className="text-foreground text-base font-semibold">
                              {domainResult.domain}
                            </h6>
                            {domainResult.best_success ? (
                              <Badge>Success</Badge>
                            ) : running ? (
                              <Badge variant="outline">Testing...</Badge>
                            ) : (
                              <Badge variant="destructive">Failed</Badge>
                            )}
                            <Badge>
                              {`${successCount}/${totalCount} configs`}
                            </Badge>
                            {domainResult.improvement &&
                              domainResult.improvement > 0 && (
                                <Badge
                                  variant="secondary"
                                  className="inline-flex items-center gap-1"
                                >
                                  <ImprovementIcon className="size-3" />
                                  {`+${domainResult.improvement.toFixed(0)}%`}
                                </Badge>
                              )}
                            <h6
                              className={cn(
                                "text-base font-semibold",
                                domainResult.best_success
                                  ? "text-secondary"
                                  : "text-muted-foreground",
                              )}
                            >
                              {domainResult.best_success
                                ? `${(
                                    domainResult.best_speed /
                                    1024 /
                                    1024
                                  ).toFixed(2)} MB/s`
                                : running
                                  ? `${totalCount} tested...`
                                  : "No working config"}
                            </h6>
                          </div>
                        </div>
                      </CollapsibleTrigger>

                      {/* Best Configuration Quick View (always visible) */}
                      {(domainResult.best_success ||
                        (running &&
                          Object.values(domainResult.results).some(
                            (r) => r.status === "complete",
                          ))) && (
                        <div>
                          <div
                            className={cn(
                              "bg-background flex items-center justify-between p-4",
                              !running && "border-border border-b",
                            )}
                          >
                            <div className="flex items-center gap-4">
                              <SpeedIcon className="text-secondary size-5" />
                              <div>
                                <p className="text-muted-foreground text-xs">
                                  {running
                                    ? "Current Best"
                                    : "Best Configuration"}
                                </p>
                                <p className="text-foreground text-base font-semibold">
                                  {domainResult.best_preset}
                                  {domainResult.best_preset &&
                                    domainResult.results[
                                      domainResult.best_preset
                                    ]?.family && (
                                      <Badge className="ml-2">
                                        {
                                          familyNames[
                                            domainResult.results[
                                              domainResult.best_preset
                                            ].family!
                                          ]
                                        }
                                      </Badge>
                                    )}
                                </p>
                              </div>
                            </div>
                            <Button
                              onClick={(e) => {
                                e.stopPropagation();
                                const bestResult =
                                  domainResult.results[
                                    domainResult.best_preset
                                  ];
                                void handleAddStrategy(
                                  domainResult.domain,
                                  bestResult,
                                );
                              }}
                              disabled={addingPreset}
                            >
                              {addingPreset ? (
                                <>
                                  <Spinner className="mr-2 size-4" />
                                  Adding...
                                </>
                              ) : (
                                <>
                                  <AddIcon className="mr-2 size-4" />
                                  {running
                                    ? "Use Current Best"
                                    : "Use This Strategy"}
                                </>
                              )}
                            </Button>
                          </div>
                          {/* Info message while still running */}
                          {running && domainResult.best_success && (
                            <Alert className="border-border rounded-none border-b">
                              <AlertDescription>
                                Found a working configuration! Still testing{" "}
                                {suite
                                  ? suite.total_checks - totalCount
                                  : "..."}{" "}
                                more configs — a faster option may be found.
                              </AlertDescription>
                            </Alert>
                          )}
                        </div>
                      )}

                      {/* Expanded Details */}
                      <CollapsibleContent>
                        <div className="p-6">
                          <Separator className="my-4" />
                          {/* Results by Phase */}
                          {(
                            [
                              "baseline",
                              "strategy_detection",
                              "optimization",
                              "combination",
                            ] as DiscoveryPhase[]
                          )
                            .filter((phase) => groupedResults[phase].length > 0)
                            .map((phase) => (
                              <div key={phase} className="mb-6">
                                <h6 className="text-muted-foreground mb-3 flex items-center gap-2 text-xs uppercase">
                                  {phaseNames[phase]}
                                  <Badge>{groupedResults[phase].length}</Badge>
                                </h6>
                                <div className="flex flex-row flex-wrap gap-2">
                                  {groupedResults[phase]
                                    .sort((a, b) => b.speed - a.speed)
                                    .map((result) => (
                                      <div
                                        key={result.preset_name}
                                        className="flex items-center gap-1"
                                      >
                                        <Badge
                                          variant={
                                            result.status === "complete"
                                              ? "default"
                                              : "destructive"
                                          }
                                        >
                                          {`${result.preset_name}: ${
                                            result.status === "complete"
                                              ? `${(
                                                  result.speed /
                                                  1024 /
                                                  1024
                                                ).toFixed(2)} MB/s`
                                              : "Failed"
                                          }`}
                                        </Badge>
                                        {result.status === "complete" &&
                                          result.preset_name !==
                                            domainResult.best_preset && (
                                            <Tooltip>
                                              <TooltipTrigger>
                                                <Button
                                                  size="sm"
                                                  variant="ghost"
                                                  onClick={() => {
                                                    void handleAddStrategy(
                                                      domainResult.domain,
                                                      result,
                                                    );
                                                  }}
                                                  disabled={addingPreset}
                                                  className="bg-muted border-border hover:bg-accent hover:border-secondary size-6 border p-0"
                                                >
                                                  <AddIcon className="size-3" />
                                                </Button>
                                              </TooltipTrigger>
                                              <TooltipContent>
                                                <p>Use this configuration</p>
                                              </TooltipContent>
                                            </Tooltip>
                                          )}
                                      </div>
                                    ))}
                                </div>
                              </div>
                            ))}
                        </div>
                      </CollapsibleContent>
                    </Collapsible>

                    {/* Failed state */}
                    {!domainResult.best_success && !running && (
                      <div className="p-6">
                        <Alert variant="destructive">
                          <AlertDescription>
                            All {Object.keys(domainResult.results).length}{" "}
                            tested configurations failed for this domain. Check
                            your network connection and domain accessibility.
                          </AlertDescription>
                        </Alert>
                      </div>
                    )}
                    {!domainResult.best_success && running && (
                      <div className="bg-background p-4">
                        <p className="text-muted-foreground flex items-center gap-2 text-sm">
                          <Spinner />
                          {suite && suite.total_checks > totalCount
                            ? `${
                                suite.total_checks - totalCount
                              } more configurations to test...`
                            : "Testing configurations..."}
                        </p>
                      </div>
                    )}
                  </Card>
                );
              })}
          </div>
        )}

      <DiscoveryAddDialog
        open={addDialog.open}
        domain={addDialog.domain}
        presetName={addDialog.presetName}
        setConfig={addDialog.setConfig}
        onClose={() =>
          setAddDialog({
            open: false,
            domain: "",
            presetName: "",
            setConfig: null,
          })
        }
        onAddNew={(name: string, domain: string) => {
          void handleAddNew(name, domain);
        }}
        onAddToExisting={(setId: string, domain: string) => {
          void handleAddToExisting(setId, domain);
        }}
        loading={addingPreset}
      />
    </div>
  );
};

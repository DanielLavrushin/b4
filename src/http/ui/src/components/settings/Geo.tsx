import { GeodatIcon, DownloadIcon, SuccessIcon } from "@b4.icons";
import { geodatApi, GeodatSource, GeoFileInfo } from "@b4.settings";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@design/components/ui/select";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import { cn } from "@design/lib/utils";
import { B4Config } from "@models/config";
import { useCallback, useEffect, useState } from "react";

export interface GeoSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | number) => void;
  loadConfig: () => void;
}

export const GeoSettings = ({ config, loadConfig }: GeoSettingsProps) => {
  const [sources, setSources] = useState<GeodatSource[]>([]);
  const [selectedSource, setSelectedSource] = useState<string>("");
  const [customGeositeURL, setCustomGeositeURL] = useState<string>("");
  const [customGeoipURL, setCustomGeoipURL] = useState<string>("");
  const [downloading, setDownloading] = useState(false);
  const [downloadStatus, setDownloadStatus] = useState<string>("");
  const [destPath, setDestPath] = useState<string>("/etc/b4");
  const [geositeInfo, setGeositeInfo] = useState<GeoFileInfo>({
    exists: false,
  });
  const [geoipInfo, setGeoipInfo] = useState<GeoFileInfo>({ exists: false });

  useEffect(() => {
    void loadSources();
    setDestPath(extractDir(config.system.geo.sitedat_path) || "/etc/b4");
  }, [config.system.geo.sitedat_path]);

  const checkFileStatus = useCallback(async () => {
    if (config.system.geo.sitedat_path) {
      try {
        const info = await geodatApi.info(config.system.geo.sitedat_path);
        setGeositeInfo(info);
      } catch {
        setGeositeInfo({ exists: false });
      }
    }

    if (config.system.geo.ipdat_path) {
      try {
        const info = await geodatApi.info(config.system.geo.ipdat_path);
        setGeoipInfo(info);
      } catch {
        setGeoipInfo({ exists: false });
      }
    }
  }, [config.system.geo.sitedat_path, config.system.geo.ipdat_path]);

  useEffect(() => {
    void checkFileStatus();
  }, [checkFileStatus]);

  const loadSources = async () => {
    try {
      const data = await geodatApi.sources();
      setSources(data);
      if (data.length > 0) {
        setSelectedSource(data[0].name);
      }
    } catch (error) {
      console.error("Failed to load geodat sources:", error);
    }
  };

  const handleSourceChange = (sourceName: string) => {
    setSelectedSource(sourceName);
    setCustomGeositeURL("");
    setCustomGeoipURL("");
  };

  const handleDownload = async () => {
    let geositeURL = customGeositeURL;
    let geoipURL = customGeoipURL;

    if (!customGeositeURL || !customGeoipURL) {
      const source = sources.find((s) => s.name === selectedSource);
      if (source) {
        geositeURL = source.geosite_url;
        geoipURL = source.geoip_url;
      }
    }

    if (!geositeURL || !geoipURL) {
      setDownloadStatus("Please select a source or enter custom URLs");
      return;
    }

    setDownloading(true);
    setDownloadStatus("Downloading...");

    try {
      const result = await geodatApi.download(geositeURL, geoipURL, destPath);
      setDownloadStatus(
        `Downloaded successfully to ${extractDir(result.geosite_path)}`
      );
      loadConfig();
      setTimeout(() => setDownloadStatus(""), 5000);
      void checkFileStatus();
    } catch (error) {
      setDownloadStatus(`Error: ${String(error)}`);
    } finally {
      setDownloading(false);
    }
  };

  const extractDir = (path: string): string => {
    if (!path) return "";
    const lastSlash = path.lastIndexOf("/");
    return lastSlash > 0 ? path.substring(0, lastSlash) : path;
  };

  const formatFileSize = (bytes?: number): string => {
    if (!bytes) return "Unknown";
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(2)} MB`;
  };

  const formatDate = (dateStr?: string): string => {
    if (!dateStr) return "Unknown";
    try {
      return new Date(dateStr).toLocaleString();
    } catch {
      return "Unknown";
    }
  };

  return (
    <div className="space-y-6">
      <Alert>
        <AlertDescription>
          <h6 className="text-sm font-semibold mb-2">
            Download GeoSite/GeoIP database files for domain and IP
            categorization.
          </h6>
          <p className="text-xs text-muted-foreground">
            Files will be saved to <strong>{destPath}</strong>
          </p>
        </AlertDescription>
      </Alert>

      {/* Current Files Status */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <GeodatIcon className="h-5 w-5" />
            <CardTitle>Current Files</CardTitle>
          </div>
          <CardDescription>
            Status of currently configured geodat files
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="p-4 rounded-md border border-border bg-card">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h6 className="text-sm font-semibold">Geosite Database</h6>
                  {geositeInfo.exists ? (
                    <Badge
                      variant="secondary"
                      className="inline-flex items-center gap-1"
                    >
                      <SuccessIcon className="h-3 w-3" />
                      Active
                    </Badge>
                  ) : (
                    <Badge variant="outline">
                      Not Found
                    </Badge>
                  )}
                </div>

                <p className="text-xs text-muted-foreground">Path</p>
                <p className="font-mono text-xs break-all">
                  {config.system.geo.sitedat_path || "Not configured"}
                </p>

                {config.system.geo.sitedat_url && (
                  <>
                    <p className="text-xs text-muted-foreground">Source</p>
                    <p className="font-mono text-xs break-all">
                      {config.system.geo.sitedat_url}
                    </p>
                  </>
                )}

                {geositeInfo.exists && (
                  <>
                    <Separator className="my-1" />
                    <div className="flex justify-between">
                      <p className="text-xs text-muted-foreground">
                        Size: {formatFileSize(geositeInfo.size)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {formatDate(geositeInfo.last_modified)}
                      </p>
                    </div>
                  </>
                )}
              </div>
            </div>

            <div className="p-4 rounded-md border border-border bg-card">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h6 className="text-sm font-semibold">GeoIP Database</h6>
                  {geoipInfo.exists ? (
                    <Badge
                      variant="secondary"
                      className="inline-flex items-center gap-1"
                    >
                      <SuccessIcon className="h-3 w-3" />
                      Active
                    </Badge>
                  ) : (
                    <Badge variant="outline">
                      Not Found
                    </Badge>
                  )}
                </div>

                <p className="text-xs text-muted-foreground">Path</p>
                <p className="font-mono text-xs break-all">
                  {config.system.geo.ipdat_path || "Not configured"}
                </p>

                {config.system.geo.ipdat_url && (
                  <>
                    <p className="text-xs text-muted-foreground">Source</p>
                    <p className="font-mono text-xs break-all">
                      {config.system.geo.ipdat_url}
                    </p>
                  </>
                )}

                {geoipInfo.exists && (
                  <>
                    <Separator className="my-1" />
                    <div className="flex justify-between">
                      <p className="text-xs text-muted-foreground">
                        Size: {formatFileSize(geoipInfo.size)}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {formatDate(geoipInfo.last_modified)}
                      </p>
                    </div>
                  </>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Download Section */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <DownloadIcon className="h-5 w-5" />
            <CardTitle>Download Files</CardTitle>
          </div>
          <CardDescription>
            Select a preset source or enter custom URLs
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="flex flex-col gap-1.5">
              <Label>Preset Source</Label>
              <Select value={selectedSource} onValueChange={handleSourceChange}>
                <SelectTrigger>
                  <SelectValue placeholder="Choose a preset source" />
                </SelectTrigger>
                <SelectContent>
                  {sources.map((source) => (
                    <SelectItem key={source.name} value={source.name}>
                      {source.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                Select a predefined geodat source
              </p>
            </div>
            <Field>
              <FieldLabel>Destination Path</FieldLabel>
              <Input
                value={destPath}
                onChange={(e) => {
                  setDestPath(e.target.value);
                }}
                placeholder="/etc/b4"
              />
              <FieldDescription>
                Directory where files will be saved
              </FieldDescription>
            </Field>

            <div className="relative my-4 md:col-span-2 flex items-center">
              <Separator className="absolute inset-0 top-1/2" />
              <span className="text-xs font-medium text-muted-foreground px-2 uppercase bg-card relative mx-auto block w-fit">
                Custom URLs
              </span>
            </div>

            <Field>
              <FieldLabel>Custom Geosite URL</FieldLabel>
              <Input
                value={customGeositeURL}
                onChange={(e) => {
                  setCustomGeositeURL(e.target.value);
                  if (e.target.value) setSelectedSource("");
                }}
                placeholder="https://example.com/geosite.dat"
              />
              <FieldDescription>Full URL to geosite.dat file</FieldDescription>
            </Field>

            <Field>
              <FieldLabel>Custom GeoIP URL</FieldLabel>
              <Input
                value={customGeoipURL}
                onChange={(e) => {
                  setCustomGeoipURL(e.target.value);
                  if (e.target.value) setSelectedSource("");
                }}
                placeholder="https://example.com/geoip.dat"
              />
              <FieldDescription>Full URL to geoip.dat file</FieldDescription>
            </Field>

            <div className="col-span-1 md:col-span-2">
              <div className="flex items-center gap-4">
                <Button
                  onClick={() => void handleDownload()}
                  disabled={downloading}
                >
                  {downloading ? (
                    <>
                      <Spinner className="h-4 w-4 mr-2" />
                      Downloading...
                    </>
                  ) : (
                    <>
                      <DownloadIcon className="h-4 w-4 mr-2" />
                      Download Files
                    </>
                  )}
                </Button>

                {downloadStatus && (
                  <p
                    className={cn(
                      "text-sm",
                      downloadStatus.includes("✓") ||
                        downloadStatus.includes("successfully")
                        ? "text-secondary"
                        : "text-destructive"
                    )}
                  >
                    {downloadStatus}
                  </p>
                )}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

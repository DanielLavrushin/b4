import { DownloadIcon, GeodatIcon, SuccessIcon } from "@b4.icons";
import { geodatApi, GeodatSource, GeoFileInfo } from "@b4.settings";
import { cn } from "@design/lib/utils";
import { B4Config } from "@models/config";
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
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSeparator,
} from "@primitives/field";
import { Input } from "@primitives/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@primitives/select";
import { Separator } from "@primitives/separator";
import { Spinner } from "@primitives/spinner";
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
        `Downloaded successfully to ${extractDir(result.geosite_path)}`,
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
    <div className="flex flex-col gap-6">
      {/* Current Files Status */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <GeodatIcon />
            Current Files
          </CardTitle>

          <CardDescription>
            Status of currently configured geodat files
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div className="border-border bg-card rounded-md border p-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h6 className="text-sm font-semibold">Geosite Database</h6>
                  {geositeInfo.exists ? (
                    <Badge variant="secondary">
                      <SuccessIcon />
                      Active
                    </Badge>
                  ) : (
                    <Badge variant="outline">Not Found</Badge>
                  )}
                </div>

                <p className="text-muted-foreground text-xs">Path</p>
                <p className="font-mono text-xs break-all">
                  {config.system.geo.sitedat_path || "Not configured"}
                </p>

                {config.system.geo.sitedat_url && (
                  <>
                    <p className="text-muted-foreground text-xs">Source</p>
                    <p className="font-mono text-xs break-all">
                      {config.system.geo.sitedat_url}
                    </p>
                  </>
                )}

                {geositeInfo.exists && (
                  <>
                    <Separator className="my-4" />
                    <div className="flex justify-between">
                      <p className="text-muted-foreground text-xs">
                        Size: {formatFileSize(geositeInfo.size)}
                      </p>
                      <p className="text-muted-foreground text-xs">
                        {formatDate(geositeInfo.last_modified)}
                      </p>
                    </div>
                  </>
                )}
              </div>
            </div>

            <div className="border-border bg-card rounded-md border p-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h6 className="text-sm font-semibold">GeoIP Database</h6>
                  {geoipInfo.exists ? (
                    <Badge
                      variant="secondary"
                      className="inline-flex items-center gap-1"
                    >
                      <SuccessIcon className="size-3" />
                      Active
                    </Badge>
                  ) : (
                    <Badge variant="outline">Not Found</Badge>
                  )}
                </div>

                <p className="text-muted-foreground text-xs">Path</p>
                <p className="font-mono text-xs break-all">
                  {config.system.geo.ipdat_path || "Not configured"}
                </p>

                {config.system.geo.ipdat_url && (
                  <>
                    <p className="text-muted-foreground text-xs">Source</p>
                    <p className="font-mono text-xs break-all">
                      {config.system.geo.ipdat_url}
                    </p>
                  </>
                )}

                {geoipInfo.exists && (
                  <>
                    <Separator className="my-4" />
                    <div className="flex justify-between">
                      <p className="text-muted-foreground text-xs">
                        Size: {formatFileSize(geoipInfo.size)}
                      </p>
                      <p className="text-muted-foreground text-xs">
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
          <CardTitle className="flex items-center gap-2">
            <DownloadIcon />
            Download Files
          </CardTitle>
          <CardDescription>
            Select a preset source or enter custom URLs
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup className="flex-row gap-4">
            <Field>
              <FieldLabel>Preset Source</FieldLabel>
              <Select
                value={selectedSource}
                onValueChange={(value) => handleSourceChange(value || "")}
              >
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
              <FieldDescription>
                Select a predefined geodat source
              </FieldDescription>
            </Field>
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
          </FieldGroup>

          <FieldSeparator className="my-6">Custom URLs</FieldSeparator>

          <FieldGroup>
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

            <Field orientation="horizontal">
              <Button
                onClick={() => void handleDownload()}
                disabled={downloading}
              >
                {downloading ? (
                  <>
                    <Spinner />
                    Downloading...
                  </>
                ) : (
                  <>
                    <DownloadIcon />
                    Download Files
                  </>
                )}
              </Button>
            </Field>
            {downloadStatus && (
              <p
                className={cn(
                  "text-sm",
                  downloadStatus.includes("✓") ||
                    downloadStatus.includes("successfully")
                    ? "text-secondary"
                    : "text-destructive",
                )}
              >
                {downloadStatus}
              </p>
            )}
          </FieldGroup>
        </CardContent>
      </Card>
    </div>
  );
};

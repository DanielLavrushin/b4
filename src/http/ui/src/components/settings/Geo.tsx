import { B4Config } from "@models/config";
import {
  Grid,
  Stack,
  Typography,
  Button,
  MenuItem,
  CircularProgress,
  Box,
  Chip,
  Divider,
} from "@mui/material";
import { DomainIcon, DownloadIcon, SuccessIcon, UploadIcon } from "@b4.icons";
import { B4Alert, B4FormGroup, B4Hint, B4Section, B4Switch, B4TextField } from "@b4.elements";
import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { geodatApi, GeodatSource, GeoFileInfo, SettingsPropHandlerType } from "@b4.settings";

const CUSTOM_SOURCE = "__custom__";

interface GeoFileCardProps {
  title: string;
  fileInfo: GeoFileInfo;
  configPath: string;
  configUrl: string;
  sources: GeodatSource[];
  sourceUrlKey: "geosite_url" | "geoip_url";
  selectedSource: string;
  onSourceChange: (value: string) => void;
  customURL: string;
  onCustomURLChange: (value: string) => void;
  downloading: boolean;
  uploading: boolean;
  status: string;
  onDownload: () => void;
  onUpload: (file: File) => void;
}

const GeoFileCard = ({
  title,
  fileInfo,
  configPath,
  configUrl,
  sources,
  sourceUrlKey,
  selectedSource,
  onSourceChange,
  customURL,
  onCustomURLChange,
  downloading,
  uploading,
  status,
  onDownload,
  onUpload,
}: GeoFileCardProps) => {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const formatFileSize = (bytes?: number): string => {
    if (!bytes) return t("core.unknown");
    const mb = bytes / (1024 * 1024);
    return `${mb.toFixed(2)} MB`;
  };

  const formatDate = (dateStr?: string): string => {
    if (!dateStr) return t("core.unknown");
    try {
      return new Date(dateStr).toLocaleString();
    } catch {
      return t("core.unknown");
    }
  };

  return (
    <Box
      sx={{
        p: 2,
        borderRadius: 1,
        border: `1px solid ${colors.border.default}`,
        bgcolor: colors.background.paper,
        height: "100%",
      }}
    >
      <Stack spacing={1.5}>
        {/* Status header */}
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <Typography variant="subtitle2" fontWeight={600}>
            {title}
          </Typography>
          {fileInfo.exists ? (
            <Chip
              size="small"
              icon={<SuccessIcon />}
              label={t("settings.Geo.active")}
              sx={{
                bgcolor: colors.accent.secondary,
                color: colors.secondary,
              }}
            />
          ) : (
            <Chip
              size="small"
              label={t("settings.Geo.notFound")}
              sx={{ bgcolor: colors.accent.tertiary }}
            />
          )}
        </Box>

        {/* File metadata */}
        <Typography
          variant="body2"
          sx={{
            fontFamily: "monospace",
            fontSize: "0.8rem",
            wordBreak: "break-all",
          }}
        >
          {configPath || t("settings.Geo.notConfigured")}
        </Typography>

        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ wordBreak: "break-all" }}
        >
          {t("settings.Geo.source")}: {configUrl || (fileInfo.exists ? t("settings.Geo.sourceLocal") : t("settings.Geo.notSet"))}
        </Typography>

        {fileInfo.exists && (
          <Box sx={{ display: "flex", justifyContent: "space-between" }}>
            <Typography variant="caption" color="text.secondary">
              {formatFileSize(fileInfo.size)}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {formatDate(fileInfo.last_modified)}
            </Typography>
          </Box>
        )}

        <Divider />

        {/* Source selection */}
        <B4TextField
          select
          label={t("settings.Geo.source")}
          value={selectedSource}
          onChange={(e) => onSourceChange(e.target.value)}
          helperText={sourceUrlKey === "geosite_url" ? t("settings.Geo.selectGeositeSource") : t("settings.Geo.selectGeoipSource")}
        >
          {sources.map((source) => (
            <MenuItem key={source.name} value={source.name}>
              {source.name}
            </MenuItem>
          ))}
          <MenuItem value={CUSTOM_SOURCE}>
            <em>{t("settings.Geo.customUrl")}</em>
          </MenuItem>
        </B4TextField>

        {/* Custom URL (conditional) */}
        {selectedSource === CUSTOM_SOURCE && (
          <B4TextField
            label={t("settings.Geo.customUrlLabel")}
            value={customURL}
            onChange={(e) => onCustomURLChange(e.target.value)}
            placeholder={`https://example.com/${sourceUrlKey === "geosite_url" ? "geosite" : "geoip"}.dat`}
            helperText={t("settings.Geo.customUrlHelp")}
          />
        )}

        {/* Download & Upload buttons */}
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
          <Button
            variant="contained"
            size="small"
            startIcon={
              downloading ? (
                <CircularProgress size={16} />
              ) : (
                <DownloadIcon />
              )
            }
            onClick={onDownload}
            disabled={
              downloading ||
              uploading ||
              (selectedSource === CUSTOM_SOURCE && !customURL) ||
              !selectedSource
            }
          >
            {downloading ? t("settings.Geo.updating") : t("settings.Geo.update")}
          </Button>
          <Button
            variant="outlined"
            size="small"
            startIcon={
              uploading ? (
                <CircularProgress size={16} />
              ) : (
                <UploadIcon />
              )
            }
            onClick={() => fileInputRef.current?.click()}
            disabled={downloading || uploading}
          >
            {uploading ? t("settings.Geo.uploading") : t("core.upload")}
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            accept=".dat,.db"
            hidden
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) onUpload(file);
              e.target.value = "";
            }}
          />
          {status && (
            <Typography
              variant="body2"
              sx={{
                color: status.toLowerCase().includes("error")
                  ? "#f44336"
                  : colors.secondary,
                fontWeight: 600,
                width: "100%",
                mt: 0.5,
              }}
            >
              {status}
            </Typography>
          )}
        </Box>
      </Stack>
    </Box>
  );
};

export interface GeoSettingsProps {
  config: B4Config;
  loadConfig: () => void;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const GeoSettings = ({ config, loadConfig, onChange }: GeoSettingsProps) => {
  const { t } = useTranslation();
  const [sources, setSources] = useState<GeodatSource[]>([]);
  const [destPath, setDestPath] = useState<string>("/etc/b4");

  // Geosite state
  const [geositeInfo, setGeositeInfo] = useState<GeoFileInfo>({
    exists: false,
  });
  const [geositeSource, setGeositeSource] = useState<string>("");
  const [geositeCustomURL, setGeositeCustomURL] = useState<string>("");
  const [geositeDownloading, setGeositeDownloading] = useState(false);
  const [geositeUploading, setGeositeUploading] = useState(false);
  const [geositeStatus, setGeositeStatus] = useState<string>("");

  // GeoIP state
  const [geoipInfo, setGeoipInfo] = useState<GeoFileInfo>({ exists: false });
  const [geoipSource, setGeoipSource] = useState<string>("");
  const [geoipCustomURL, setGeoipCustomURL] = useState<string>("");
  const [geoipDownloading, setGeoipDownloading] = useState(false);
  const [geoipUploading, setGeoipUploading] = useState(false);
  const [geoipStatus, setGeoipStatus] = useState<string>("");

  // Filter sources per file type
  const geositeSources = useMemo(
    () => sources.filter((s) => s.geosite_url !== ""),
    [sources],
  );
  const geoipSources = useMemo(
    () => sources.filter((s) => s.geoip_url !== ""),
    [sources],
  );

  useEffect(() => {
    void loadSources();
    const dir = extractDir(config.system.geo.sitedat_path);
    setDestPath(dir.startsWith("/") ? dir : "/etc/b4");
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
    } catch (error) {
      console.error("Failed to load geodat sources:", error);
    }
  };

  // Match dropdown selection to the URL stored in config
  useEffect(() => {
    if (geositeSources.length === 0 || geositeSource) return;
    const configUrl = config.system.geo.sitedat_url;
    if (configUrl) {
      const match = geositeSources.find((s) => s.geosite_url === configUrl);
      if (match) {
        setGeositeSource(match.name);
      } else {
        setGeositeSource(CUSTOM_SOURCE);
        setGeositeCustomURL(configUrl);
      }
    } else if (!config.system.geo.sitedat_path) {
      setGeositeSource(geositeSources[0].name);
    }
  }, [geositeSources, geositeSource, config.system.geo.sitedat_url, config.system.geo.sitedat_path]);

  useEffect(() => {
    if (geoipSources.length === 0 || geoipSource) return;
    const configUrl = config.system.geo.ipdat_url;
    if (configUrl) {
      const match = geoipSources.find((s) => s.geoip_url === configUrl);
      if (match) {
        setGeoipSource(match.name);
      } else {
        setGeoipSource(CUSTOM_SOURCE);
        setGeoipCustomURL(configUrl);
      }
    } else if (!config.system.geo.ipdat_path) {
      setGeoipSource(geoipSources[0].name);
    }
  }, [geoipSources, geoipSource, config.system.geo.ipdat_url, config.system.geo.ipdat_path]);

  const extractDir = (path: string): string => {
    if (!path?.startsWith("/")) return "";
    const lastSlash = path.lastIndexOf("/");
    return lastSlash > 0 ? path.substring(0, lastSlash) : "/";
  };

  const handleGeositeSourceChange = (value: string) => {
    setGeositeSource(value);
    if (value !== CUSTOM_SOURCE) setGeositeCustomURL("");
  };

  const handleGeoipSourceChange = (value: string) => {
    setGeoipSource(value);
    if (value !== CUSTOM_SOURCE) setGeoipCustomURL("");
  };

  const handleGeositeDownload = async () => {
    const url =
      geositeSource === CUSTOM_SOURCE
        ? geositeCustomURL
        : geositeSources.find((s) => s.name === geositeSource)?.geosite_url;

    if (!url) {
      setGeositeStatus(t("settings.Geo.selectSource"));
      return;
    }

    setGeositeDownloading(true);
    setGeositeStatus(t("settings.Geo.downloadingGeosite"));

    try {
      await geodatApi.download(destPath, url);
      setGeositeStatus(t("settings.Geo.downloadSuccess"));
      loadConfig();
      void checkFileStatus();
      setTimeout(() => setGeositeStatus(""), 5000);
    } catch (error) {
      setGeositeStatus(`Error: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setGeositeDownloading(false);
    }
  };

  const handleGeoipDownload = async () => {
    const url =
      geoipSource === CUSTOM_SOURCE
        ? geoipCustomURL
        : geoipSources.find((s) => s.name === geoipSource)?.geoip_url;

    if (!url) {
      setGeoipStatus(t("settings.Geo.selectSource"));
      return;
    }

    setGeoipDownloading(true);
    setGeoipStatus(t("settings.Geo.downloadingGeoip"));

    try {
      await geodatApi.download(destPath, undefined, url);
      setGeoipStatus(t("settings.Geo.downloadSuccess"));
      loadConfig();
      void checkFileStatus();
      setTimeout(() => setGeoipStatus(""), 5000);
    } catch (error) {
      setGeoipStatus(`Error: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setGeoipDownloading(false);
    }
  };

  const handleGeositeUpload = async (file: File) => {
    setGeositeUploading(true);
    setGeositeStatus(t("settings.Geo.uploadingGeosite"));
    try {
      await geodatApi.upload(file, "geosite", destPath);
      setGeositeStatus(t("settings.Geo.uploadSuccess"));
      setGeositeSource("");
      loadConfig();
      void checkFileStatus();
      setTimeout(() => setGeositeStatus(""), 5000);
    } catch (error) {
      setGeositeStatus(`Error: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setGeositeUploading(false);
    }
  };

  const handleGeoipUpload = async (file: File) => {
    setGeoipUploading(true);
    setGeoipStatus(t("settings.Geo.uploadingGeoip"));
    try {
      await geodatApi.upload(file, "geoip", destPath);
      setGeoipStatus(t("settings.Geo.uploadSuccess"));
      setGeoipSource("");
      loadConfig();
      void checkFileStatus();
      setTimeout(() => setGeoipStatus(""), 5000);
    } catch (error) {
      setGeoipStatus(`Error: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setGeoipUploading(false);
    }
  };

  return (
    <Stack spacing={3}>
      <B4Alert>
        <Typography variant="subtitle2" gutterBottom>
          {t("settings.Geo.alert")}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {t("settings.Geo.alertSub")}
        </Typography>
      </B4Alert>

      <B4Section
        title={t("settings.Geo.title")}
        description={t("settings.Geo.description")}
        icon={<DomainIcon />}
      >
        <B4TextField
          label={t("settings.Geo.destDir")}
          value={destPath}
          onChange={(e) => setDestPath(e.target.value)}
          placeholder="/etc/b4"
          helperText={t("settings.Geo.destDirHelp")}
        />

        <B4FormGroup label={t("settings.Geo.autoUpdate")} columns={2}>
          <B4Switch
            label={t("settings.Geo.autoUpdateOnStartup")}
            checked={config.system.geo.auto_update?.on_startup ?? false}
            onChange={(checked: boolean) =>
              onChange("system.geo.auto_update.on_startup", checked)
            }
            description={t("settings.Geo.autoUpdateOnStartupDesc")}
          />
          <B4TextField
            select
            label={t("settings.Geo.autoUpdateInterval")}
            value={config.system.geo.auto_update?.interval ?? ""}
            onChange={(e) =>
              onChange("system.geo.auto_update.interval", e.target.value)
            }
            helperText={t("settings.Geo.autoUpdateIntervalHelp")}
          >
            <MenuItem value="">{t("settings.Geo.autoUpdateOff")}</MenuItem>
            <MenuItem value="daily">{t("settings.Geo.autoUpdateDaily")}</MenuItem>
            <MenuItem value="weekly">{t("settings.Geo.autoUpdateWeekly")}</MenuItem>
            <MenuItem value="monthly">{t("settings.Geo.autoUpdateMonthly")}</MenuItem>
          </B4TextField>
        </B4FormGroup>
        {config.system.geo.auto_update?.last_run && (
          <B4Hint>
            {t("settings.Geo.autoUpdateLastRun")}:{" "}
            {new Date(config.system.geo.auto_update.last_run).toLocaleString()}
          </B4Hint>
        )}

        <Grid container spacing={2} sx={{ mt: 1 }}>
          <Grid size={{ xs: 12, md: 6 }}>
            <GeoFileCard
              title={t("settings.Geo.geositeDb")}
              fileInfo={geositeInfo}
              configPath={config.system.geo.sitedat_path}
              configUrl={config.system.geo.sitedat_url}
              sources={geositeSources}
              sourceUrlKey="geosite_url"
              selectedSource={geositeSource}
              onSourceChange={handleGeositeSourceChange}
              customURL={geositeCustomURL}
              onCustomURLChange={setGeositeCustomURL}
              downloading={geositeDownloading}
              uploading={geositeUploading}
              status={geositeStatus}
              onDownload={() => void handleGeositeDownload()}
              onUpload={(file) => void handleGeositeUpload(file)}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <GeoFileCard
              title={t("settings.Geo.geoipDb")}
              fileInfo={geoipInfo}
              configPath={config.system.geo.ipdat_path}
              configUrl={config.system.geo.ipdat_url}
              sources={geoipSources}
              sourceUrlKey="geoip_url"
              selectedSource={geoipSource}
              onSourceChange={handleGeoipSourceChange}
              customURL={geoipCustomURL}
              onCustomURLChange={setGeoipCustomURL}
              downloading={geoipDownloading}
              uploading={geoipUploading}
              status={geoipStatus}
              onDownload={() => void handleGeoipDownload()}
              onUpload={(file) => void handleGeoipUpload(file)}
            />
          </Grid>
        </Grid>
      </B4Section>
    </Stack>
  );
};

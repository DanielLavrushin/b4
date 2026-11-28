import { useState, useEffect, useMemo, useDeferredValue } from "react";
import type { ClipboardEvent } from "react";
import { Box, Button, Stack, Typography } from "@mui/material";
import { Link } from "react-router";
import {
  ImportExportIcon,
  CopyIcon,
  DownloadIcon,
  CheckCircleIcon,
  WarningIcon,
} from "@b4.icons";
import {
  B4Alert,
  B4FieldMessage,
  B4Section,
  B4TextField,
} from "@b4.elements";
import { useCaptures } from "@b4.capture";
import { useSnackbar } from "@context/SnackbarProvider";
import { useTranslation, Trans } from "react-i18next";

import { B4SetConfig } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { HubWarning, formatWarningParam, isHubEnvelope } from "@models/hub";
import { hubApi } from "@api/hub";
import { ApiError } from "@api/apiClient";
import {
  collectPayloadRefs,
  compactJson,
  convertLegacySetShape,
  copyText,
  exportSetJson,
  isPlainObject,
  keepLocalDscp,
  mergeHubLink,
  payloadBaseName,
  sanitizeProbeUrls,
} from "@utils";

type Obj = Record<string, unknown>;

const SET_ROOT_KEYS = ["name", "tcp", "udp", "targets", "fragmentation", "faking"];

const isWholeSetJson = (value: unknown) =>
  isHubEnvelope(value) ||
  (isPlainObject(value) && SET_ROOT_KEYS.some((key) => key in value));

function mergeObjectWithDefaults(partial: Obj, defaults: Obj): Obj {
  const result = { ...defaults };
  for (const [key, value] of Object.entries(partial)) {
    result[key] = key in result ? mergeWithDefaults(value, result[key]) : value;
  }
  return result;
}

function mergeWithDefaults(partial: unknown, defaults: unknown): unknown {
  if (partial === undefined || partial === null) return defaults;
  if (Array.isArray(defaults)) {
    return Array.isArray(partial) ? partial : defaults;
  }
  if (isPlainObject(defaults)) {
    return isPlainObject(partial)
      ? mergeObjectWithDefaults(partial, defaults)
      : defaults;
  }
  return partial;
}

function importedDiscovery(
  value: unknown,
  current: B4SetConfig["discovery"],
): B4SetConfig["discovery"] {
  const imported =
    isPlainObject(value) && Array.isArray(value.urls)
      ? sanitizeProbeUrls(
          value.urls.filter((u): u is string => typeof u === "string"),
        )
      : null;
  const discovery: NonNullable<B4SetConfig["discovery"]> = {
    urls: imported ?? [...(current?.urls ?? [])],
  };
  if (current?.watchdog) discovery.watchdog = true;
  return discovery;
}

interface ImportExportSettingsProps {
  config: B4SetConfig;
  deviceFilter: boolean;
  onImport: (importedConfig: B4SetConfig) => void;
}

export const ImportExportSettings = ({
  config,
  deviceFilter,
  onImport,
}: ImportExportSettingsProps) => {
  const { t } = useTranslation();
  const exportJson = useMemo(() => exportSetJson(config, 2), [config]);
  const [jsonValue, setJsonValue] = useState(exportJson);
  const [jsonEdited, setJsonEdited] = useState(false);
  const [syncedExport, setSyncedExport] = useState(exportJson);
  const [importedExport, setImportedExport] = useState<string | null>(null);
  const [importSuccess, setImportSuccess] = useState(false);
  const [envelopeImported, setEnvelopeImported] = useState(false);
  const [importWarnings, setImportWarnings] = useState<HubWarning[]>([]);
  const [importedPayloadRefs, setImportedPayloadRefs] = useState<string[]>([]);
  const [capturesReady, setCapturesReady] = useState(false);
  const { showSuccess, showError } = useSnackbar();
  const { captures, loadCaptures } = useCaptures();
  const hasSourceDevices = (config.targets.source_devices ?? []).length > 0;
  const deferredJson = useDeferredValue(jsonValue);
  const jsonParse = useMemo(() => {
    if (!deferredJson.trim()) return { ok: false as const, error: "" };
    try {
      return { ok: true as const, value: JSON.parse(deferredJson) as unknown };
    } catch (error) {
      return {
        ok: false as const,
        error: error instanceof Error ? error.message : String(error),
      };
    }
  }, [deferredJson]);
  const clearImportOutcome = () => {
    setImportSuccess(false);
    setEnvelopeImported(false);
    setImportWarnings([]);
    setImportedPayloadRefs([]);
  };

  if (exportJson !== syncedExport) {
    setSyncedExport(exportJson);
    setImportedExport(null);
    if (!jsonEdited) setJsonValue(exportJson);
    if (exportJson !== importedExport) clearImportOutcome();
  }

  const markImported = (imported: B4SetConfig) => {
    setJsonEdited(false);
    setImportedExport(exportSetJson(imported, 2));
  };

  useEffect(() => {
    void loadCaptures().then(() => setCapturesReady(true));
  }, [loadCaptures]);

  const missingPayloads = useMemo(() => {
    if (!capturesReady || importedPayloadRefs.length === 0) return [];
    const available = new Set(captures.map((c) => payloadBaseName(c.filepath)));
    return importedPayloadRefs.filter(
      (ref) => !available.has(payloadBaseName(ref)),
    );
  }, [capturesReady, captures, importedPayloadRefs]);

  function migrateSetConfig(set: Record<string, unknown>): B4SetConfig {
    const tcp = set.tcp as Record<string, unknown> | undefined;

    if (tcp) {
      if (!tcp.incoming) {
        tcp.incoming = {
          mode: "off",
          min: 14,
          max: 14,
          fake_ttl: 3,
          fake_count: 3,
          strategy: "badsum",
        };
      }

      const wssize = tcp.wssize as Record<string, unknown> | undefined;
      if (wssize && wssize.cutoff !== "first_data") {
        wssize.cutoff = "";
      }
    }

    const frag = set.fragmentation as Record<string, unknown> | undefined;
    if (frag) {
      if (!frag.seq_overlap_pattern) {
        frag.seq_overlap_pattern = [];
      }
      if (typeof frag.seq_overlap_length !== "number") {
        frag.seq_overlap_length = 0;
      }
      delete frag.overlap;
    }

    const faking = set.faking as Record<string, unknown> | undefined;
    if (faking) {
      if (!faking.tls_mod) {
        faking.tls_mod = [];
      }
      if (!faking.payload_file) {
        faking.payload_file = "";
      }
    }

    const udp = set.udp as Record<string, unknown> | undefined;
    if (udp && (udp.filter_quic === "disabled" || udp.filter_quic === "parse")) {
      udp.filter_quic = "sni";
    }

    set.routing = {
      ...createDefaultSet(0).routing,
      ...(set.routing as Record<string, unknown>),
    };

    return set as unknown as B4SetConfig;
  }

  const importEnvelope = async (raw: unknown, text: string) => {
    if (!isHubEnvelope(raw)) return;
    try {
      const result = await hubApi.importEnvelope(raw);
      const link = mergeHubLink(config.hub, result.set.hub, true);
      const local = keepLocalDscp(config, deviceFilter);
      const parsed = {
        ...result.set,
        id: config.id,
        group: config.group,
        enabled: config.enabled,
        hub: link.hub,
        discovery: config.discovery ?? result.set.discovery,
        dscp: local.dscp ?? result.set.dscp,
        revision: config.revision,
      };
      onImport(parsed);
      markImported(parsed);
      await loadCaptures();
      setImportedPayloadRefs(collectPayloadRefs(parsed));
      setImportWarnings([
        ...(result.warnings ?? []),
        ...link.warnings,
        ...local.warnings,
      ]);
      setEnvelopeImported(true);
      setImportSuccess(true);
    } catch (e) {
      const detail = e instanceof ApiError ? e.message : String(e);
      setJsonValue(text);
      setImportSuccess(false);
      setEnvelopeImported(false);
      setImportWarnings([]);
      setImportedPayloadRefs([]);
      showError(t("sets.importExport.envelopeFailed", { error: detail }));
    }
  };

  const handleFormat = () => {
    try {
      setJsonValue(JSON.stringify(JSON.parse(jsonValue), null, 2));
      clearImportOutcome();
    } catch {
      showError(t("sets.importExport.invalidJson"));
    }
  };

  const importJson = (text: string) => {
    try {
      const raw = JSON.parse(text) as Record<string, unknown>;
      if (isHubEnvelope(raw)) {
        void importEnvelope(raw, text);
        return true;
      }
      const { b4_version: _, ...configFields } = raw;

      const defaults = createDefaultSet(0);
      const fullConfig = mergeWithDefaults(
        convertLegacySetShape(configFields),
        defaults,
      ) as Record<string, unknown>;

      const parsed = migrateSetConfig(fullConfig);

      if (
        !parsed.name ||
        !parsed.tcp ||
        !parsed.udp ||
        !parsed.fragmentation ||
        !parsed.faking ||
        !parsed.targets
      ) {
        showError(t("sets.importExport.invalidFields"));
        return false;
      }

      parsed.id = config.id;
      parsed.group = config.group;
      parsed.targets = {
        ...parsed.targets,
        ip_priority: config.targets.ip_priority ?? false,
      };
      if (config.revision) parsed.revision = config.revision;
      else delete parsed.revision;
      parsed.discovery = importedDiscovery(
        configFields.discovery,
        config.discovery,
      );
      const link = mergeHubLink(config.hub, configFields.hub);
      parsed.hub = link.hub;
      onImport(parsed);
      markImported(parsed);
      setImportedPayloadRefs(collectPayloadRefs(parsed));
      setImportWarnings(link.warnings);
      setEnvelopeImported(false);
      void loadCaptures();
      setImportSuccess(true);
      return true;
    } catch {
      showError(t("sets.importExport.invalidJson"));
      return false;
    }
  };

  const handlePaste = (e: ClipboardEvent) => {
    let pasted: unknown;
    try {
      pasted = JSON.parse(e.clipboardData.getData("text"));
    } catch {
      return;
    }
    if (!isWholeSetJson(pasted)) return;
    e.preventDefault();
    setJsonValue(JSON.stringify(pasted, null, 2));
    setJsonEdited(true);
    clearImportOutcome();
  };

  const handleImport = () => {
    importJson(jsonValue);
  };

  const handleCopy = async () => {
    const ok = await copyText(compactJson(jsonValue));
    if (ok) showSuccess(t("sets.importExport.copiedToClipboard"));
    else showError(t("sets.importExport.copyFailed"));
  };

  return (
    <B4Section
      title={t("sets.importExport.sectionTitle")}
      description={t("sets.importExport.sectionDescription")}
      icon={<ImportExportIcon />}
    >
      {importSuccess && (
        <B4Alert severity="success" icon={<CheckCircleIcon />} sx={{ mb: 2 }}>
          <Trans
            i18nKey={
              envelopeImported
                ? "sets.importExport.envelopeImported"
                : "sets.importExport.importSuccess"
            }
          />
        </B4Alert>
      )}
      {importWarnings.length > 0 && (
        <B4Alert severity="warning" icon={<WarningIcon />} sx={{ mb: 2 }}>
          <Typography variant="subtitle2">
            {t("sets.importExport.warningsTitle")}
          </Typography>
          <ul style={{ margin: "4px 0 0", paddingLeft: 20 }}>
            {importWarnings.map((w, i) => (
              <li key={`${w.code}-${i}`}>
                {t(`sets.importExport.warnings.${w.code}`, {
                  defaultValue: w.code,
                  ...Object.fromEntries(
                    Object.entries(w.params ?? {}).map(([k, v]) => [
                      k,
                      formatWarningParam(v),
                    ]),
                  ),
                })}
              </li>
            ))}
          </ul>
        </B4Alert>
      )}
      {missingPayloads.length > 0 && (
        <B4Alert severity="warning" icon={<WarningIcon />} sx={{ mb: 2 }}>
          <Trans
            i18nKey="sets.importExport.missingPayloads"
            count={missingPayloads.length}
            values={{ files: missingPayloads.join(", ") }}
          />{" "}
          <Link to="/settings/payloads">
            {t("sets.importExport.missingPayloadsLink")}
          </Link>
        </B4Alert>
      )}
      <Stack spacing={2}>
        <Box>
          <B4TextField
            label={t("sets.importExport.jsonLabel")}
            value={jsonValue}
            onFocus={(e) => (e.target as HTMLTextAreaElement).select()}
            onChange={(e) => {
              setJsonValue(e.target.value);
              setJsonEdited(e.target.value.trim() !== "");
              clearImportOutcome();
            }}
            onPaste={handlePaste}
            multiline
            rows={10}
            error={
              !jsonParse.ok && Boolean(jsonValue.trim() && jsonParse.error)
            }
            helperText={
              !jsonParse.ok && jsonValue.trim() && jsonParse.error
                ? `${t("sets.importExport.invalidJson")}: ${jsonParse.error}`
                : t("sets.importExport.jsonHelper")
            }
          />
          {hasSourceDevices && (
            <B4FieldMessage>
              {t("sets.importExport.deviceFilterWarning")}
            </B4FieldMessage>
          )}
        </Box>
        <Stack
          direction="row"
          spacing={2}
          alignItems="center"
          flexWrap="wrap"
          useFlexGap
        >
          <Button
            variant="outlined"
            startIcon={<CopyIcon />}
            onClick={() => void handleCopy()}
          >
            {t("sets.importExport.copyJson")}
          </Button>
          <Button variant="outlined" onClick={handleFormat} disabled={!jsonParse.ok}>
            {t("sets.importExport.formatJson")}
          </Button>
          <Button
            variant="outlined"
            startIcon={<DownloadIcon />}
            onClick={handleImport}
            disabled={!jsonParse.ok}
          >
            {t("sets.importExport.import")}
          </Button>
        </Stack>
      </Stack>
    </B4Section>
  );
};

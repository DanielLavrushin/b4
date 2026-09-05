import { useState, useEffect } from "react";
import {
  Box,
  Stack,
  Typography,
  Collapse,
  Autocomplete,
  ToggleButtonGroup,
  ToggleButton,
  Button,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import { CoreIcon, ExpandIcon, CollapseIcon } from "@b4.icons";
import { B4Badge, B4Slider, B4Switch, B4TextField } from "@b4.elements";
import { colors } from "@design";
import { Capture } from "@b4.capture";

export type TLSVersion = "auto" | "tls12" | "tls13";
export type IPVersion = "auto" | "ipv4" | "ipv6";

export interface DiscoveryOptions {
  checkDns: boolean;
  useCache: boolean;
  payloadFiles: string[];
  validationTries: number;
  tlsVersion: TLSVersion;
  ipVersion: IPVersion;
}

const STORAGE_KEY = "b4_discovery_options";
const EXPANDED_KEY = "b4_discovery_options_expanded";

export const defaultOptions: DiscoveryOptions = {
  checkDns: true,
  useCache: true,
  payloadFiles: [],
  validationTries: 1,
  tlsVersion: "auto",
  ipVersion: "auto",
};

const LEGACY_KEYS = {
  skipDns: "b4_discovery_skipdns",
  skipCache: "b4_discovery_skipcache",
  tries: "b4_discovery_validation_tries",
  tls: "b4_discovery_tls_version",
  ip: "b4_discovery_ip_version",
} as const;

function loadLegacyOptions(): DiscoveryOptions | null {
  const read = (key: string) => localStorage.getItem(key);
  if (Object.values(LEGACY_KEYS).every((key) => read(key) === null)) {
    return null;
  }
  const tries = Number(read(LEGACY_KEYS.tries));
  const tls = read(LEGACY_KEYS.tls);
  const ip = read(LEGACY_KEYS.ip);
  const options: DiscoveryOptions = {
    ...defaultOptions,
    checkDns: read(LEGACY_KEYS.skipDns) !== "true",
    useCache: read(LEGACY_KEYS.skipCache) !== "true",
    validationTries: tries >= 1 && tries <= 5 ? tries : 1,
    tlsVersion:
      tls === "tls12" || tls === "tls13" ? (tls) : "auto",
    ipVersion: ip === "ipv4" || ip === "ipv6" ? (ip) : "auto",
  };
  Object.values(LEGACY_KEYS).forEach((key) => localStorage.removeItem(key));
  return options;
}

export function loadOptions(): DiscoveryOptions {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      const legacy = loadLegacyOptions();
      if (legacy) saveOptions(legacy);
      return legacy ?? defaultOptions;
    }
    const parsed = JSON.parse(raw) as Partial<DiscoveryOptions>;
    return { ...defaultOptions, ...parsed, payloadFiles: [] };
  } catch {
    return defaultOptions;
  }
}

export function saveOptions(options: DiscoveryOptions) {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ ...options, payloadFiles: [] }),
    );
  } catch {
    return;
  }
}

interface DiscoveryOptionsPanelProps {
  options: DiscoveryOptions;
  onChange: (options: DiscoveryOptions) => void;
  onClearCache: () => void;
  captures: Capture[];
  disabled?: boolean;
  ipVersionEnabled?: boolean;
}

const toggleSx = {
  "& .MuiToggleButton-root": {
    color: colors.text.secondary,
    borderColor: colors.border.default,
    textTransform: "none",
    px: 2,
    "&.Mui-selected": {
      bgcolor: colors.accent.secondary,
      color: colors.secondary,
      borderColor: colors.secondary,
      "&:hover": { bgcolor: colors.accent.secondary },
    },
  },
};

export const DiscoveryOptionsPanel = ({
  options,
  onChange,
  onClearCache,
  captures,
  disabled = false,
  ipVersionEnabled = true,
}: DiscoveryOptionsPanelProps) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(
    () => localStorage.getItem(EXPANDED_KEY) === "true",
  );

  useEffect(() => {
    localStorage.setItem(EXPANDED_KEY, String(expanded));
  }, [expanded]);

  const tlsCaptures = captures.filter((c) => c.protocol === "tls");
  const summary = summarize(options, t, ipVersionEnabled);

  return (
    <Box
      sx={{
        border: `1px solid ${colors.border.default}`,
        borderRadius: 1,
        overflow: "hidden",
      }}
    >
      <Box
        onClick={() => setExpanded((e) => !e)}
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          p: 1.5,
          cursor: "pointer",
          bgcolor: colors.background.dark,
          "&:hover": { bgcolor: colors.accent.primary },
        }}
      >
        <Stack direction="row" alignItems="center" spacing={1}>
          <CoreIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("discovery.options.title")}
          </Typography>
          {!expanded && (
            <B4Badge
              label={summary || t("discovery.options.defaults")}
              sx={{
                height: 20,
                fontSize: "0.7rem",
                bgcolor: summary
                  ? colors.accent.secondary
                  : colors.background.paper,
                color: summary ? colors.secondary : colors.text.disabled,
              }}
            />
          )}
        </Stack>
        {expanded ? (
          <CollapseIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
        ) : (
          <ExpandIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
        )}
      </Box>

      <Collapse in={expanded}>
        <Box
          sx={{
            p: 3,
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
            gap: 3,
          }}
        >
          <B4Switch
            label={t("discovery.options.checkDns")}
            description={t("discovery.options.checkDnsHint")}
            checked={options.checkDns}
            onChange={(checked) => onChange({ ...options, checkDns: checked })}
            disabled={disabled}
          />

          <Box>
            <B4Switch
              label={t("discovery.options.useCache")}
              description={t("discovery.options.useCacheHint")}
              checked={options.useCache}
              onChange={(checked) =>
                onChange({ ...options, useCache: checked })
              }
              disabled={disabled}
            />
            <Button
              size="small"
              onClick={onClearCache}
              disabled={disabled}
              sx={{ textTransform: "none", ml: "50px", mt: 0.5 }}
            >
              {t("discovery.options.forget")}
            </Button>
          </Box>

          <B4Slider
            label={t("discovery.options.validationTries")}
            value={options.validationTries}
            onChange={(value: number) =>
              onChange({ ...options, validationTries: value })
            }
            min={1}
            max={5}
            step={1}
            disabled={disabled}
            helperText={t("discovery.options.validationTriesHint")}
          />

          <Box>
            <Typography variant="body1" sx={{ mb: 0.5 }}>
              {t("discovery.options.tlsVersion")}
            </Typography>
            <ToggleButtonGroup
              value={options.tlsVersion}
              exclusive
              onChange={(_, value) => {
                if (value !== null) {
                  onChange({ ...options, tlsVersion: value as TLSVersion });
                }
              }}
              disabled={disabled}
              size="small"
              sx={toggleSx}
            >
              <ToggleButton value="auto">Auto</ToggleButton>
              <ToggleButton value="tls12">TLS 1.2</ToggleButton>
              <ToggleButton value="tls13">TLS 1.3</ToggleButton>
            </ToggleButtonGroup>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 0.75, display: "block" }}
            >
              {t("discovery.options.tlsVersionHint")}
            </Typography>
          </Box>

          {ipVersionEnabled && (
            <Box>
              <Typography variant="body1" sx={{ mb: 0.5 }}>
                {t("discovery.options.ipVersion")}
              </Typography>
              <ToggleButtonGroup
                value={options.ipVersion}
                exclusive
                onChange={(_, value) => {
                  if (value !== null) {
                    onChange({ ...options, ipVersion: value as IPVersion });
                  }
                }}
                disabled={disabled}
                size="small"
                sx={toggleSx}
              >
                <ToggleButton value="auto">Auto</ToggleButton>
                <ToggleButton value="ipv4">IPv4</ToggleButton>
                <ToggleButton value="ipv6">IPv6</ToggleButton>
              </ToggleButtonGroup>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ mt: 0.75, display: "block" }}
              >
                {t("discovery.options.ipVersionHint")}
              </Typography>
            </Box>
          )}

          <Box>
            <Typography variant="body1" sx={{ mb: 0.5 }}>
              {t("discovery.options.payloads")}
            </Typography>
            {tlsCaptures.length > 0 ? (
              <>
                <Autocomplete
                  multiple
                  size="small"
                  options={tlsCaptures.map((c) => c.domain)}
                  value={options.payloadFiles}
                  onChange={(_, value) =>
                    onChange({ ...options, payloadFiles: value })
                  }
                  disabled={disabled}
                  renderInput={(params) => (
                    <B4TextField
                      {...params}
                      placeholder={
                        options.payloadFiles.length === 0
                          ? t("discovery.options.selectPayloads")
                          : ""
                      }
                      size="small"
                    />
                  )}
                  renderValue={(value, getTagProps) =>
                    value.map((domain, index) => (
                      <B4Badge
                        {...getTagProps({ index })}
                        key={domain}
                        label={domain}
                        size="small"
                        sx={{
                          bgcolor: colors.accent.secondary,
                          border: `1px solid ${colors.secondary}`,
                        }}
                      />
                    ))
                  }
                />
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ mt: 0.75, display: "block" }}
                >
                  {t("discovery.options.payloadsHint")}
                </Typography>
              </>
            ) : (
              <Typography variant="caption" color="text.secondary">
                {t("discovery.options.noPayloads")}{" "}
                <RouterLink
                  to="/settings/payloads"
                  style={{ color: colors.secondary }}
                >
                  {t("core.nav.settings")}
                </RouterLink>
              </Typography>
            )}
          </Box>
        </Box>
      </Collapse>
    </Box>
  );
};

function summarize(
  options: DiscoveryOptions,
  t: (key: string, opts?: Record<string, unknown>) => string,
  ipVersionEnabled: boolean,
): string {
  const parts: string[] = [];
  if (!options.checkDns) parts.push(t("discovery.options.summaryNoDns"));
  if (!options.useCache) parts.push(t("discovery.options.summaryNoCache"));
  if (options.tlsVersion === "tls12") parts.push("TLS 1.2");
  if (options.tlsVersion === "tls13") parts.push("TLS 1.3");
  if (ipVersionEnabled && options.ipVersion === "ipv4") parts.push("IPv4");
  if (ipVersionEnabled && options.ipVersion === "ipv6") parts.push("IPv6");
  if (options.validationTries > 1) {
    parts.push(
      t("discovery.options.tries", { count: options.validationTries }),
    );
  }
  if (options.payloadFiles.length > 0) {
    parts.push(
      t("discovery.options.summaryPayloads", {
        count: options.payloadFiles.length,
      }),
    );
  }
  return parts.join(", ");
}

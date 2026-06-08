import { useState, useEffect } from "react";
import {
  Box,
  Stack,
  Typography,
  Collapse,
  Autocomplete,
  Paper,
  ToggleButtonGroup,
  ToggleButton,
  Button,
} from "@mui/material";
import { FilterIcon, ExpandIcon, CollapseIcon } from "@b4.icons";
import {
  B4Badge,
  B4FormGroup,
  B4Slider,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { colors } from "@design";
import { Capture } from "@b4.capture";
import { useTranslation } from "react-i18next";

export type TLSVersion = "auto" | "tls12" | "tls13";
export type IPVersion = "auto" | "ipv4" | "ipv6";

export interface DiscoveryOptions {
  skipDNS: boolean;
  skipCache: boolean;
  payloadFiles: string[];
  validationTries: number;
  tlsVersion: TLSVersion;
  ipVersion: IPVersion;
}

interface DiscoveryOptionsPanelProps {
  options: DiscoveryOptions;
  onChange: (options: DiscoveryOptions) => void;
  onClearCache?: () => void;
  captures: Capture[];
  disabled?: boolean;
  ipVersionEnabled?: boolean;
}

export const DiscoveryOptionsPanel = ({
  options,
  onChange,
  onClearCache,
  captures,
  disabled = false,
  ipVersionEnabled = true,
}: DiscoveryOptionsPanelProps) => {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem("b4_discovery_options_expanded") === "true";
  });

  useEffect(() => {
    localStorage.setItem("b4_discovery_options_expanded", String(expanded));
  }, [expanded]);

  const tlsCaptures = captures.filter((c) => c.protocol === "tls");
  const hasOptions =
    options.skipDNS ||
    options.skipCache ||
    options.payloadFiles.length > 0 ||
    options.validationTries > 1 ||
    options.tlsVersion !== "auto" ||
    (ipVersionEnabled && options.ipVersion !== "auto");

  return (
    <Box
      sx={{
        border: `1px solid ${colors.border.default}`,
        borderRadius: 1,
        overflow: "hidden",
      }}
    >
      {/* Header */}
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
          <FilterIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("discovery.options.title")}
          </Typography>
          {!expanded && hasOptions && (
            <B4Badge
              label={getOptionsSummary(options, t, ipVersionEnabled)}
              sx={{
                height: 20,
                fontSize: "0.7rem",
                bgcolor: colors.accent.secondary,
                color: colors.secondary,
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

      {/* Content */}
      <Collapse in={expanded}>
        <Paper
          sx={{
            p: 3,
            bgcolor: colors.background.paper,
            border: `1px solid ${colors.border.default}`,
            display: "flex",
            flexDirection: "column",
          }}
          variant="outlined"
        >
          <B4FormGroup label={t("discovery.options.title")} columns={2}>
            <B4Switch
              label={t("discovery.options.skipDns")}
              checked={options.skipDNS}
              onChange={(checked) => onChange({ ...options, skipDNS: checked })}
              disabled={disabled}
            />

            {/* Cache Controls */}
            <Box>
              <B4Switch
                label={t("discovery.options.skipCache")}
                checked={options.skipCache}
                onChange={(checked) =>
                  onChange({ ...options, skipCache: checked })
                }
                disabled={disabled}
              />
              <Stack
                direction="row"
                alignItems="center"
                spacing={1}
                sx={{ mt: 0.5 }}
              >
                <Typography variant="caption" color="text.secondary">
                  {t("discovery.options.cacheHint")}
                </Typography>
                {onClearCache && (
                  <Button
                    size="small"
                    variant="outlined"
                    onClick={onClearCache}
                    disabled={disabled}
                  >
                    {t("discovery.options.clearCache")}
                  </Button>
                )}
              </Stack>
            </Box>

            {/* TLS Version */}
            <Box>
              <Typography variant="body1" sx={{ mb: 1 }}>
                {t("discovery.options.tlsVersion")}
              </Typography>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ mb: 1, display: "block" }}
              >
                {t("discovery.options.tlsVersionHint")}
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
                sx={{
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
                }}
              >
                <ToggleButton value="auto">Auto</ToggleButton>
                <ToggleButton value="tls12">TLS 1.2</ToggleButton>
                <ToggleButton value="tls13">TLS 1.3</ToggleButton>
              </ToggleButtonGroup>
            </Box>

            {/* IP Version */}
            {ipVersionEnabled && (
              <Box>
                <Typography variant="body1" sx={{ mb: 1 }}>
                  {t("discovery.options.ipVersion")}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ mb: 1, display: "block" }}
                >
                  {t("discovery.options.ipVersionHint")}
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
                  sx={{
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
                  }}
                >
                  <ToggleButton value="auto">Auto</ToggleButton>
                  <ToggleButton value="ipv4">IPv4</ToggleButton>
                  <ToggleButton value="ipv6">IPv6</ToggleButton>
                </ToggleButtonGroup>
              </Box>
            )}

            {/* Custom Payloads */}
            {tlsCaptures.length > 0 && (
              <Box>
                <Typography variant="body1" sx={{ mb: 1 }}>
                  {t("discovery.options.customPayloads")}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ mb: 1, display: "block" }}
                >
                  {t("discovery.options.customPayloadsHint")}
                </Typography>
                <Autocomplete
                  multiple
                  size="small"
                  options={tlsCaptures.map((c) => c.domain)}
                  value={options.payloadFiles}
                  onChange={(_, newValue) =>
                    onChange({ ...options, payloadFiles: newValue })
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
              </Box>
            )}
            {/* Validation Tries */}
            <Box>
              <B4Slider
                label={t("discovery.options.validationTries")}
                value={options.validationTries}
                onChange={(value: number) =>
                  onChange({ ...options, validationTries: value })
                }
                min={1}
                max={5}
                step={1}
                helperText={t("discovery.options.validationTriesHint")}
              />
            </Box>

            {tlsCaptures.length === 0 && (
              <Typography variant="caption" color="text.secondary">
                {t("discovery.options.noPayloads")}{" "}
                <a
                  href="/settings/payloads"
                  style={{ color: colors.secondary }}
                >
                  {t("discovery.options.capturePayloads")}
                </a>{" "}
                {t("discovery.options.noPayloadsSuffix")}
              </Typography>
            )}
          </B4FormGroup>
        </Paper>
      </Collapse>
    </Box>
  );
};

function getOptionsSummary(
  options: DiscoveryOptions,
  t: (key: string, opts?: Record<string, unknown>) => string,
  ipVersionEnabled: boolean,
): string {
  const parts: string[] = [];
  if (options.skipDNS) parts.push(t("discovery.options.summarySkipDns"));
  if (options.skipCache) parts.push(t("discovery.options.summarySkipCache"));
  if (options.tlsVersion === "tls12") parts.push("TLS 1.2");
  if (options.tlsVersion === "tls13") parts.push("TLS 1.3");
  if (ipVersionEnabled && options.ipVersion === "ipv4") parts.push("IPv4");
  if (ipVersionEnabled && options.ipVersion === "ipv6") parts.push("IPv6");
  if (options.validationTries > 1)
    parts.push(
      t("discovery.options.summaryTries", { count: options.validationTries }),
    );
  if (options.payloadFiles.length > 0) {
    parts.push(
      options.payloadFiles.length > 1
        ? t("discovery.options.summaryPayloads_plural", {
            count: options.payloadFiles.length,
          })
        : t("discovery.options.summaryPayloads", {
            count: options.payloadFiles.length,
          }),
    );
  }
  return parts.join(", ");
}

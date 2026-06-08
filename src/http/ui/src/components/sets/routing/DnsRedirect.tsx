import { useState } from "react";
import {
  Box,
  Grid,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@mui/material";
import { B4Alert, B4Badge, B4Hint, B4Switch, B4TextField } from "@b4.elements";
import { DnsIcon, CheckIcon, BlockIcon } from "@b4.icons";
import { B4SetConfig } from "@models/config";
import { colors } from "@design";
import { useTranslation } from "react-i18next";
import dns from "@assets/dns.json";
import doh from "@assets/doh.json";

interface DnsEntry {
  name: string;
  ip: string;
  ipv6?: boolean;
  desc: string;
  warn?: boolean;
}

interface DohEntry {
  name: string;
  url: string;
  desc: string;
  warn?: boolean;
}

const POPULAR_DNS = (dns as DnsEntry[]).sort((a, b) =>
  a.name.localeCompare(b.name),
);

const POPULAR_DOH = doh as DohEntry[];

type ResolverMode = "udp" | "doh";

interface DnsRedirectProps {
  config: B4SetConfig;
  ipv6: boolean;
  onChange: (
    field: string,
    value: string | number | boolean | string[] | number[] | null | undefined,
  ) => void;
}

export const DnsRedirect = ({ config, ipv6, onChange }: DnsRedirectProps) => {
  const { t } = useTranslation();
  const dnsConfig = config.dns || {
    enabled: false,
    target_dns: "",
    doh_url: "",
    fragment_query: false,
  };

  const [mode, setMode] = useState<ResolverMode>(
    dnsConfig.doh_url ? "doh" : "udp",
  );

  const handleModeChange = (_: unknown, next: ResolverMode | null) => {
    if (!next || next === mode) return;
    setMode(next);
    if (next === "udp") {
      onChange("dns.doh_url", "");
    } else {
      onChange("dns.target_dns", "");
    }
  };

  const selectedServer = POPULAR_DNS.find((d) => d.ip === dnsConfig.target_dns);
  const selectedDoH = POPULAR_DOH.find((d) => d.url === dnsConfig.doh_url);
  const activeResolver = mode === "doh" ? dnsConfig.doh_url : dnsConfig.target_dns;

  return (
    <Grid container spacing={3}>
      <B4Hint>{t("sets.dns.alert")}</B4Hint>

      <Grid size={{ lg: 12 }}>
        <B4Switch
          label={t("sets.dns.enable")}
          checked={dnsConfig.enabled}
          onChange={(checked: boolean) => onChange("dns.enabled", checked)}
          description={t("sets.dns.enableDesc")}
        />
      </Grid>

      {dnsConfig.enabled && (
        <>
          <Grid size={{ xs: 12 }}>
            <Typography
              variant="caption"
              color="text.secondary"
              component="div"
              sx={{ mb: 1 }}
            >
              {t("sets.dns.modeLabel")}
            </Typography>
            <ToggleButtonGroup
              exclusive
              size="small"
              value={mode}
              onChange={handleModeChange}
              sx={{
                "& .MuiToggleButton-root.Mui-selected": {
                  bgcolor: `${colors.secondary}22`,
                  color: colors.secondary,
                  borderColor: colors.secondary,
                  "&:hover": { bgcolor: `${colors.secondary}33` },
                },
              }}
            >
              <ToggleButton value="udp">{t("sets.dns.modeUdp")}</ToggleButton>
              <ToggleButton value="doh">{t("sets.dns.modeDoH")}</ToggleButton>
            </ToggleButtonGroup>
          </Grid>

          {mode === "udp" && (
            <>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Switch
                  label={t("sets.dns.fragmentQuery")}
                  checked={dnsConfig.fragment_query || false}
                  onChange={(checked: boolean) =>
                    onChange("dns.fragment_query", checked)
                  }
                  description={t("sets.dns.fragmentQueryDesc")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4TextField
                  label={t("sets.dns.serverIp")}
                  value={dnsConfig.target_dns}
                  onChange={(e) => onChange("dns.target_dns", e.target.value)}
                  placeholder={t("sets.dns.serverIpPlaceholder")}
                  helperText={t("sets.dns.serverIpHelper")}
                />
              </Grid>

              <Grid size={{ xs: 12, md: 6 }}>
                {selectedServer && (
                  <Box
                    sx={{
                      p: 2,
                      bgcolor: colors.background.paper,
                      borderRadius: 1,
                      border: `1px solid ${colors.border.default}`,
                      height: "100%",
                    }}
                  >
                    <Stack direction="row" alignItems="center" spacing={1}>
                      <DnsIcon sx={{ color: colors.secondary }} />
                      <Typography variant="subtitle2">
                        {selectedServer.name}
                      </Typography>
                    </Stack>
                    <Typography variant="caption" color="text.secondary">
                      {selectedServer.desc}
                    </Typography>
                  </Box>
                )}
              </Grid>

              <Grid size={{ xs: 12 }}>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  {t("sets.dns.recommendedServers")}
                </Typography>
                <Box
                  sx={{
                    border: `1px solid ${colors.border.default}`,
                    borderRadius: 1,
                    bgcolor: colors.background.paper,
                    maxHeight: 320,
                    overflow: "auto",
                  }}
                >
                  <List dense disablePadding>
                    {POPULAR_DNS.filter((server) =>
                      ipv6 ? server.ipv6 : !server.ipv6,
                    ).map((server) => (
                      <ListItemButton
                        key={server.ip}
                        selected={dnsConfig.target_dns === server.ip}
                        onClick={() => onChange("dns.target_dns", server.ip)}
                        sx={{
                          borderLeft: server.warn
                            ? `3px solid ${colors.quaternary}`
                            : "3px solid transparent",
                          "&.Mui-selected": {
                            bgcolor: `${colors.secondary}22`,
                            borderLeftColor: colors.secondary,
                            "&:hover": { bgcolor: `${colors.secondary}33` },
                          },
                        }}
                      >
                        <ListItemIcon sx={{ minWidth: 36 }}>
                          {(() => {
                            if (dnsConfig.target_dns === server.ip) {
                              return (
                                <CheckIcon
                                  sx={{ color: colors.secondary, fontSize: 20 }}
                                />
                              );
                            }
                            if (server.warn) {
                              return (
                                <BlockIcon
                                  sx={{ color: colors.secondary, fontSize: 20 }}
                                />
                              );
                            }
                            return (
                              <DnsIcon
                                sx={{ color: colors.text.secondary, fontSize: 20 }}
                              />
                            );
                          })()}
                        </ListItemIcon>
                        <ListItemText
                          primary={
                            <Stack
                              direction="row"
                              alignItems="center"
                              spacing={1}
                            >
                              <Typography
                                variant="body2"
                                sx={{
                                  fontFamily: "monospace",
                                  color: server.warn
                                    ? colors.secondary
                                    : "inherit",
                                }}
                              >
                                {server.name}
                              </Typography>
                              <Typography variant="body2" color="text.secondary">
                                {server.ip}
                              </Typography>
                            </Stack>
                          }
                          secondary={server.desc}
                          slotProps={{
                            secondary: {
                              variant: "caption",
                              sx: {
                                color: server.warn ? colors.secondary : undefined,
                              },
                            },
                          }}
                        />
                      </ListItemButton>
                    ))}
                  </List>
                </Box>
              </Grid>
            </>
          )}

          {mode === "doh" && (
            <>
              <Grid size={{ xs: 12 }}>
                <B4TextField
                  label={t("sets.dns.dohUrl")}
                  value={dnsConfig.doh_url || ""}
                  onChange={(e) => onChange("dns.doh_url", e.target.value)}
                  placeholder={t("sets.dns.dohUrlPlaceholder")}
                  helperText={t("sets.dns.dohUrlHelper")}
                />
              </Grid>

              <Grid size={{ xs: 12, md: 6 }}>
                {selectedDoH && (
                  <Box
                    sx={{
                      p: 2,
                      bgcolor: colors.background.paper,
                      borderRadius: 1,
                      border: `1px solid ${colors.border.default}`,
                      height: "100%",
                    }}
                  >
                    <Stack direction="row" alignItems="center" spacing={1}>
                      <DnsIcon sx={{ color: colors.secondary }} />
                      <Typography variant="subtitle2">
                        {selectedDoH.name}
                      </Typography>
                    </Stack>
                    <Typography variant="caption" color="text.secondary">
                      {selectedDoH.desc}
                    </Typography>
                  </Box>
                )}
              </Grid>

              <Grid size={{ xs: 12 }}>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  {t("sets.dns.recommendedDoH")}
                </Typography>
                <Box
                  sx={{
                    border: `1px solid ${colors.border.default}`,
                    borderRadius: 1,
                    bgcolor: colors.background.paper,
                    maxHeight: 320,
                    overflow: "auto",
                  }}
                >
                  <List dense disablePadding>
                    {POPULAR_DOH.map((server) => (
                      <ListItemButton
                        key={server.url}
                        selected={dnsConfig.doh_url === server.url}
                        onClick={() => onChange("dns.doh_url", server.url)}
                        sx={{
                          borderLeft: "3px solid transparent",
                          "&.Mui-selected": {
                            bgcolor: `${colors.secondary}22`,
                            borderLeftColor: colors.secondary,
                            "&:hover": { bgcolor: `${colors.secondary}33` },
                          },
                        }}
                      >
                        <ListItemIcon sx={{ minWidth: 36 }}>
                          {dnsConfig.doh_url === server.url ? (
                            <CheckIcon
                              sx={{ color: colors.secondary, fontSize: 20 }}
                            />
                          ) : (
                            <DnsIcon
                              sx={{ color: colors.text.secondary, fontSize: 20 }}
                            />
                          )}
                        </ListItemIcon>
                        <ListItemText
                          primary={
                            <Stack
                              direction="row"
                              alignItems="center"
                              spacing={1}
                            >
                              <Typography variant="body2">
                                {server.name}
                              </Typography>
                            </Stack>
                          }
                          secondary={
                            <Stack component="span">
                              <Typography
                                variant="caption"
                                color="text.secondary"
                                sx={{ fontFamily: "monospace" }}
                              >
                                {server.url}
                              </Typography>
                              <Typography
                                variant="caption"
                                color="text.secondary"
                              >
                                {server.desc}
                              </Typography>
                            </Stack>
                          }
                          slotProps={{ secondary: { component: "div" } }}
                        />
                      </ListItemButton>
                    ))}
                  </List>
                </Box>
              </Grid>
            </>
          )}

          {/* Visual explanation */}
          <Grid size={{ xs: 12 }}>
            <Box
              sx={{
                p: 2,
                bgcolor: colors.background.paper,
                borderRadius: 1,
                border: `1px solid ${colors.border.default}`,
              }}
            >
              <Typography
                variant="caption"
                color="text.secondary"
                component="div"
                sx={{ mb: 1 }}
              >
                {t("sets.dns.howItWorks")}
              </Typography>
              <Stack
                direction="row"
                alignItems="center"
                spacing={1}
                flexWrap="wrap"
                useFlexGap
              >
                <B4Badge
                  label={t("sets.dns.vizApp")}
                  sx={{ bgcolor: colors.accent.primary }}
                />
                <Typography variant="caption">
                  {t("sets.dns.vizQueryFor")}
                </Typography>
                <B4Badge
                  label="instagram.com"
                  size="small"
                  sx={{
                    bgcolor: colors.accent.secondary,
                    color: colors.secondary,
                  }}
                />
                <Typography variant="caption">→</Typography>
                <B4Badge
                  label={t("sets.dns.vizPoisoned")}
                  size="small"
                  sx={{
                    bgcolor: colors.quaternary,
                    textDecoration: "line-through",
                  }}
                />
                <Typography variant="caption">→</Typography>
                <B4Badge
                  label={activeResolver || t("sets.dns.vizSelectDns")}
                  size="small"
                  sx={{
                    bgcolor: activeResolver
                      ? colors.tertiary
                      : colors.accent.primary,
                  }}
                />
                <Typography variant="caption">
                  {t("sets.dns.vizRealIp")}
                </Typography>
              </Stack>
            </Box>
          </Grid>

          {/* Warnings */}
          {!activeResolver && (
            <B4Alert severity="warning" sx={{ m: 0 }}>
              {mode === "doh"
                ? t("sets.dns.noDohWarning")
                : t("sets.dns.noServerWarning")}
            </B4Alert>
          )}

          {mode === "udp" && dnsConfig.target_dns === "8.8.8.8" && (
            <B4Alert severity="warning" sx={{ m: 0 }}>
              {t("sets.dns.googleWarning")}
            </B4Alert>
          )}
        </>
      )}
    </Grid>
  );
};

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
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
import {
  B4Alert,
  B4Badge,
  B4FormHeader,
  B4Hint,
  B4ResolverInput,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { DnsIcon, CheckIcon, BlockIcon, LockIcon } from "@b4.icons";
import { B4SetConfig } from "@models/config";
import { colors, fonts } from "@design";
import { useTranslation } from "react-i18next";
import { setsApi } from "@api/sets";
import { useChangedFields } from "@context/ChangedFieldsContext";
import {
  MAX_SET_RESOLVERS,
  askedResolvers,
  isResolverAddress,
  misplacedResolver,
  parsePins,
  pinsToText,
  resolverError,
  resolverFields,
  resolverKey,
  resolverLabel,
  resolverList,
  samePins,
  type ResolverSource,
} from "@utils";
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

type ServerKind = "dns" | "doh";

interface RecommendedServer {
  kind: ServerKind;
  value: string;
  name: string;
  desc: string;
  ipv6?: boolean;
  warn?: boolean;
}

const POPULAR_DNS = (dns as DnsEntry[]).sort((a, b) =>
  a.name.localeCompare(b.name),
);

const POPULAR_DOH = doh as DohEntry[];

const RECOMMENDED: RecommendedServer[] = [
  ...POPULAR_DNS.map(
    ({ ip, ...server }): RecommendedServer => ({
      ...server,
      kind: "dns",
      value: ip,
    }),
  ),
  ...POPULAR_DOH.map(
    ({ url, ...server }): RecommendedServer => ({
      ...server,
      kind: "doh",
      value: url,
    }),
  ),
];

const RECOMMENDED_BY_KEY = new Map(
  RECOMMENDED.map((server) => [resolverKey(server.value), server]),
);

const recommendedServer = (entry: string) =>
  RECOMMENDED_BY_KEY.get(resolverKey(entry));

interface DnsRedirectProps {
  config: B4SetConfig;
  ipv6: boolean;
  onChange: (
    field: string,
    value:
      | string
      | number
      | boolean
      | string[]
      | number[]
      | Record<string, string[]>
      | null
      | undefined,
  ) => void;
}

export const DnsRedirect = ({ config, ipv6, onChange }: DnsRedirectProps) => {
  const { t } = useTranslation();
  const dnsConfig = config.dns || {
    enabled: false,
    target_dns: "",
    doh_url: "",
    fragment_query: false,
    strict: false,
  };

  const resolvers = resolverList(dnsConfig);
  const misplaced = resolverKey(misplacedResolver(dnsConfig));
  const asked = askedResolvers(dnsConfig);
  const chosen = new Set(asked.map(resolverKey));
  const listFull = resolvers.length >= MAX_SET_RESOLVERS;
  const [serverKind, setServerKind] = useState<ServerKind | "all">("all");

  const { changedValues } = useChangedFields();
  const savedKeys = useMemo(() => {
    const saved = (path: string, current: unknown) => {
      const leaf = changedValues.get(path);
      return leaf ? leaf.before : current;
    };
    const before = {
      doh_url: saved("dns.doh_url", dnsConfig.doh_url),
      target_dns: saved("dns.target_dns", dnsConfig.target_dns),
      upstreams: saved("dns.upstreams", dnsConfig.upstreams),
    } as ResolverSource;
    return new Set(resolverList(before).map(resolverKey));
  }, [
    changedValues,
    dnsConfig.doh_url,
    dnsConfig.target_dns,
    dnsConfig.upstreams,
  ]);
  const isNewResolver = useCallback(
    (entry: string) => !savedKeys.has(resolverKey(entry)),
    [savedKeys],
  );

  const writeResolvers = (next: string[]) => {
    const fields = resolverFields(next);
    onChange("dns.doh_url", fields.doh_url);
    onChange("dns.target_dns", fields.target_dns);
    onChange("dns.upstreams", fields.upstreams);
  };

  const toggleServer = (value: string) => {
    const key = resolverKey(value);
    const others = resolvers.filter((entry) => resolverKey(entry) !== key);
    writeResolvers(chosen.has(key) ? others : [...others, value]);
  };

  const resolverProblem = useCallback(
    (entry: string) => {
      const problem =
        resolverError(entry) ??
        (resolverKey(entry) === misplaced ? "misplaced" : null);
      return problem ? t(`sets.dns.resolverErrors.${problem}`, { entry }) : null;
    },
    [t, misplaced],
  );

  const chipProblem = (entry: string) =>
    resolverProblem(entry) ??
    (chosen.has(resolverKey(entry))
      ? null
      : t("core.resolverInput.overLimit", { max: MAX_SET_RESOLVERS }));

  const resolverPaths = useMemo(
    () => [
      { path: "dns.doh_url", label: t("sets.dns.dohUrl") },
      { path: "dns.target_dns", label: t("sets.dns.targetDns") },
      { path: "dns.upstreams", label: t("sets.dns.upstreams") },
    ],
    [t],
  );

  const [pinsText, setPinsText] = useState(() => pinsToText(dnsConfig.pins));
  const [seenPins, setSeenPins] = useState(dnsConfig.pins);
  if (dnsConfig.pins !== seenPins) {
    setSeenPins(dnsConfig.pins);
    if (!samePins(parsePins(pinsText), dnsConfig.pins)) {
      setPinsText(pinsToText(dnsConfig.pins));
    }
  }
  const [uncovered, setUncovered] = useState<string[]>([]);

  const handlePinsChange = (text: string) => {
    setPinsText(text);
    onChange("dns.pins", parsePins(text));
  };

  const addUncoveredToTargets = () => {
    const existing = config.targets?.sni_domains ?? [];
    onChange("targets.sni_domains", [
      ...existing,
      ...uncovered.filter((d) => !existing.includes(d)),
    ]);
    setUncovered([]);
  };

  useEffect(() => {
    const domains = Object.keys(parsePins(pinsText));
    if (domains.length === 0 || !config.id) {
      setUncovered([]);
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      void (async () => {
        const missing: string[] = [];
        for (const domain of domains) {
          try {
            const matches = await setsApi.checkDomain(domain);
            if (!matches.some((m) => m.set_id === config.id)) {
              missing.push(domain);
            }
          } catch {
            return;
          }
        }
        if (!cancelled) setUncovered(missing);
      })();
    }, 700);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [pinsText, config.id]);

  const shownServers = RECOMMENDED.filter(
    (server) =>
      (ipv6 || !server.ipv6) &&
      (serverKind === "all" || server.kind === serverKind),
  );
  const [shownResolver, ...otherResolvers] = asked;

  const serverIcon = (server: RecommendedServer, selected: boolean) => {
    if (selected) {
      return <CheckIcon sx={{ color: colors.secondary, fontSize: 20 }} />;
    }
    if (server.warn) {
      return <BlockIcon sx={{ color: colors.secondary, fontSize: 20 }} />;
    }
    const Icon = server.kind === "doh" ? LockIcon : DnsIcon;
    return <Icon sx={{ color: colors.text.secondary, fontSize: 20 }} />;
  };

  return (
    <Grid container spacing={3}>
      <B4Hint>{t("sets.dns.alert")}</B4Hint>

      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.dns.enable")}
          checked={dnsConfig.enabled}
          path="dns.enabled"
          onChange={(checked: boolean) => onChange("dns.enabled", checked)}
          description={t("sets.dns.enableDesc")}
        />
      </Grid>

      {dnsConfig.enabled && (
        <>
          <Grid size={{ xs: 12 }}>
            <B4Switch
              checked={dnsConfig.strict || false}
              path="dns.strict"
              onChange={(checked: boolean) => onChange("dns.strict", checked)}
              description={t("sets.dns.strictDesc")}
            />
          </Grid>

          <Grid size={{ xs: 12 }}>
            <B4ResolverInput
              label={t("sets.dns.resolvers")}
              value={resolvers}
              onChange={writeResolvers}
              validate={resolverProblem}
              chipProblem={chipProblem}
              highlight={isNewResolver}
              max={MAX_SET_RESOLVERS}
              paths={resolverPaths}
              lookup={recommendedServer}
              placeholder={t("sets.dns.resolversPlaceholder")}
              helperText={t("sets.dns.resolversHelper")}
              emptyMessage={t("sets.dns.resolversEmpty")}
              aiTopic="dns.upstreams"
            />
          </Grid>

          {asked.some(isResolverAddress) && (
            <Grid size={{ xs: 12 }}>
              <B4Switch
                checked={dnsConfig.fragment_query || false}
                path="dns.fragment_query"
                onChange={(checked: boolean) =>
                  onChange("dns.fragment_query", checked)
                }
                description={t("sets.dns.fragmentQueryDesc")}
              />
            </Grid>
          )}

          <Grid size={{ xs: 12 }} onMouseDown={(e) => e.preventDefault()}>
            <Stack
              direction="row"
              alignItems="center"
              justifyContent="space-between"
              flexWrap="wrap"
              useFlexGap
              spacing={1}
              sx={{ mb: 1 }}
            >
              <Typography variant="subtitle2">
                {t("sets.dns.recommendedServers")}
              </Typography>
              <ToggleButtonGroup
                exclusive
                size="small"
                aria-label={t("sets.dns.recommendedServers")}
                value={serverKind}
                onChange={(_, next: ServerKind | "all" | null) => {
                  if (next) setServerKind(next);
                }}
                sx={{
                  "& .MuiToggleButton-root.Mui-selected": {
                    bgcolor: `${colors.secondary}22`,
                    color: colors.secondary,
                    "&:hover": { bgcolor: `${colors.secondary}33` },
                  },
                }}
              >
                <ToggleButton value="all">
                  {t("sets.dns.serversAll")}
                </ToggleButton>
                <ToggleButton value="dns">
                  {t("sets.dns.serversPlain")}
                </ToggleButton>
                <ToggleButton value="doh">
                  {t("sets.dns.serversDoH")}
                </ToggleButton>
              </ToggleButtonGroup>
            </Stack>
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
                {shownServers.map((server) => {
                  const selected = chosen.has(resolverKey(server.value));
                  return (
                    <ListItemButton
                      key={server.value}
                      selected={selected}
                      aria-pressed={selected}
                      disabled={listFull && !selected}
                      onClick={() => toggleServer(server.value)}
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
                        {serverIcon(server, selected)}
                      </ListItemIcon>
                      <ListItemText
                        primary={
                          <Stack
                            direction="row"
                            alignItems="baseline"
                            flexWrap="wrap"
                            columnGap={1}
                          >
                            <Typography
                              variant="body2"
                              sx={{
                                color: server.warn
                                  ? colors.secondary
                                  : "inherit",
                              }}
                            >
                              {server.name}
                            </Typography>
                            <Typography
                              variant="body2"
                              color="text.secondary"
                              sx={{
                                fontFamily: fonts.mono,
                                wordBreak: "break-all",
                              }}
                            >
                              {server.value}
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
                  );
                })}
              </List>
            </Box>
          </Grid>

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
                  label={
                    shownResolver
                      ? resolverLabel(shownResolver)
                      : t("sets.dns.vizSelectDns")
                  }
                  size="small"
                  sx={{
                    bgcolor: shownResolver
                      ? colors.tertiary
                      : colors.accent.primary,
                  }}
                />
                {otherResolvers.length > 0 && (
                  <B4Badge
                    label={`+${otherResolvers.length}`}
                    size="small"
                    sx={{ bgcolor: colors.tertiary }}
                  />
                )}
                <Typography variant="caption">
                  {t("sets.dns.vizRealIp")}
                </Typography>
              </Stack>
            </Box>
          </Grid>

          {asked.length === 0 && (
            <B4Alert severity="warning" sx={{ m: 0 }}>
              {t("sets.dns.noResolverWarning")}
            </B4Alert>
          )}

          {chosen.has("8.8.8.8") && (
            <B4Alert severity="warning" sx={{ m: 0 }}>
              {t("sets.dns.googleWarning")}
            </B4Alert>
          )}
        </>
      )}

      <B4FormHeader label={t("sets.dns.stripEchHeader")} />

      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.dns.stripEch")}
          checked={dnsConfig.strip_ech || false}
          onChange={(checked: boolean) => onChange("dns.strip_ech", checked)}
          description={t("sets.dns.stripEchDesc")}
          aiTopic="dns.strip_ech"
        />
      </Grid>

      <B4FormHeader label={t("sets.dns.pinsHeader")} />

      <Grid size={{ xs: 12 }}>
        <B4Hint sx={{ mb: 2 }}>{t("sets.dns.pinsAlert")}</B4Hint>
        <B4TextField
          label={t("sets.dns.pins")}
          value={pinsText}
          path="dns.pins"
          onChange={(e) => handlePinsChange(e.target.value)}
          placeholder={t("sets.dns.pinsPlaceholder")}
          multiline
          minRows={3}
          maxRows={10}
          helperText={t("sets.dns.pinsHelper")}
          aiTopic="dns.pins"
        />

        {uncovered.length > 0 && (
          <B4Alert severity="warning" sx={{ mt: 2 }}>
            <Stack spacing={1} alignItems="flex-start">
              <span>
                {t("sets.dns.pinsNotTargeted", {
                  domains: uncovered.join(", "),
                })}
              </span>
              <Button
                size="small"
                variant="outlined"
                onClick={addUncoveredToTargets}
              >
                {t("sets.dns.pinsAddToTargets")}
              </Button>
            </Stack>
          </B4Alert>
        )}
      </Grid>
    </Grid>
  );
};

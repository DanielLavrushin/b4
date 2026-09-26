import { useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Radio,
  Stack,
  Typography,
} from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import * as ipaddr from "ipaddr.js";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { AddIcon, IpIcon, IpInfoIcon, RefreshIcon } from "@b4.icons";
import { B4Alert, B4Badge, B4Dialog, B4TextField } from "@b4.elements";
import { colors, radius, typography } from "@design";
import { ApiError } from "@api/apiClient";
import { asnApi } from "@api/asn";
import { AsnFacts, AsnLargeNetworkAlert } from "@common/AsnFacts";
import { SetSelector } from "@common/SetSelector";
import { useAsnCache } from "@hooks/useAsn";
import {
  IpTarget,
  IpTargetRequest,
  ipTargetLabel,
} from "@hooks/useIpActions";
import {
  AsnLookupOrigin,
  AsnView,
  formatAsn,
  normalizeAsn,
} from "@models/asn";
import { B4SetConfig, CREATE_SET_SENTINEL } from "@models/config";
import {
  asnLabel,
  asnStorage,
  generateIpVariants,
  localizeApiError,
} from "@utils";
import { IpInfo, fetchIpInfo, ipInfoLocation } from "./ipinfo";

interface AddIpDialogProps {
  open: boolean;
  ip: string;
  asn?: string;
  asnName?: string;
  sets: B4SetConfig[];
  ipInfoToken?: string;
  onClose: () => void;
  onSubmit: (request: IpTargetRequest) => Promise<unknown>;
  onAddHostname?: (hostname: string) => void;
}

interface CidrChoice {
  key: string;
  target: Extract<IpTarget, { kind: "cidr" }>;
  hint: string;
}

const ASN_CHOICE = "asn";
const SINGLE_CHOICE = "single";
const LOOKUP_STALE_MS = 5 * 60 * 1000;
const RESOLVE_STALE_MS = 60 * 1000;

function canonicalCidr(cidr: string): string | null {
  try {
    const [addr, bits] = ipaddr.parseCIDR(cidr);
    const network =
      addr.kind() === "ipv4"
        ? ipaddr.IPv4.networkAddressFromCIDR(cidr)
        : ipaddr.IPv6.networkAddressFromCIDR(cidr);
    return `${network.toString()}/${bits}`;
  } catch {
    return null;
  }
}

function addressKind(ip: string): "ipv4" | "ipv6" | null {
  try {
    return ipaddr.process(ip).kind();
  } catch {
    return null;
  }
}

function coveringPrefix(prefix: string, ip: string): string | null {
  const canonical = prefix ? canonicalCidr(prefix) : null;
  if (!canonical) return null;
  const [addr, bits] = ipaddr.parseCIDR(canonical);
  const kind = addr.kind();
  if (kind !== addressKind(ip)) return null;
  const hostBits = kind === "ipv4" ? 32 : 128;
  return bits < hostBits ? canonical : null;
}

function variantHint(t: TFunction, cidr: string): string {
  const bits = Number(cidr.split("/")[1]);
  if (cidr.includes(":")) {
    if (bits === 64) return t("connections.addIp.ipv6Subnet");
    if (bits === 48) return t("connections.addIp.ipv6Site");
    return t("connections.addIp.ipv6IspRange");
  }
  if (bits === 24) return t("connections.addIp.subnet256");
  if (bits === 16) return t("connections.addIp.network65k");
  if (bits === 8) return t("connections.addIp.classA");
  return "";
}

function buildCidrChoices(
  t: TFunction,
  ip: string,
  prefix: string,
): CidrChoice[] {
  const variants = generateIpVariants(ip);
  const choices: CidrChoice[] = [
    {
      key: SINGLE_CHOICE,
      target: { kind: "cidr", value: variants[0] ?? ip, label: ip },
      hint: t("connections.addIp.singleIp"),
    },
  ];
  const announced = coveringPrefix(prefix, ip);
  const wider = variants
    .slice(1)
    .map((variant) => canonicalCidr(variant) ?? variant);
  if (announced && !wider.includes(announced)) {
    choices.push({
      key: announced,
      target: { kind: "cidr", value: announced, label: announced },
      hint: t("connections.addIp.announcedPrefix"),
    });
  }
  for (const cidr of wider) {
    const hint = variantHint(t, cidr);
    choices.push({
      key: cidr,
      target: { kind: "cidr", value: cidr, label: cidr },
      hint:
        cidr === announced
          ? [t("connections.addIp.announcedPrefix"), hint]
              .filter(Boolean)
              .join(" · ")
          : hint,
    });
  }
  return choices;
}

const radioSx = {
  color: colors.border.default,
  "&.Mui-checked": { color: colors.primary },
};

const choiceButtonSx = {
  borderRadius: 1,
  "&.Mui-selected": {
    bgcolor: colors.accent.primary,
    "&:hover": { bgcolor: colors.accent.primaryHover },
  },
};

export const AddIpDialog = ({
  open,
  ip,
  asn,
  asnName,
  sets,
  ipInfoToken,
  onClose,
  onSubmit,
  onAddHostname,
}: AddIpDialogProps) => {
  const { t } = useTranslation();
  const { store: storeAsnView } = useAsnCache();
  const preferredAsn = asn ? normalizeAsn(asn) : null;

  const [choiceKey, setChoiceKey] = useState<string | null>(null);
  const [pickedAsn, setPickedAsn] = useState<string | null>(null);
  const [setChoice, setSetChoice] = useState<{ id: string; name: string } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const [ipInfo, setIpInfo] = useState<IpInfo | null>(null);
  const [ipInfoBusy, setIpInfoBusy] = useState(false);
  const [ipInfoError, setIpInfoError] = useState("");

  const lookupQuery = useQuery({
    queryKey: ["asn-lookup", ip],
    queryFn: () => asnApi.lookup(ip),
    enabled: open && !!ip,
    retry: false,
    staleTime: LOOKUP_STALE_MS,
    refetchOnWindowFocus: false,
  });
  const lookup = lookupQuery.data;

  const cidrChoices = useMemo(
    () => buildCidrChoices(t, ip, lookup?.prefix ?? ""),
    [t, ip, lookup?.prefix],
  );

  const origins = useMemo(() => {
    const list: AsnLookupOrigin[] = [...(lookup?.asns ?? [])];
    if (preferredAsn && !list.some((o) => o.id === preferredAsn)) {
      list.unshift({ id: preferredAsn, name: asnName ?? "", cached: false });
    }
    return list;
  }, [lookup?.asns, preferredAsn, asnName]);

  const asnId =
    (pickedAsn && origins.some((o) => o.id === pickedAsn) ? pickedAsn : null) ??
    (preferredAsn && origins.some((o) => o.id === preferredAsn)
      ? preferredAsn
      : null) ??
    origins[0]?.id ??
    null;
  const origin = origins.find((o) => o.id === asnId);

  const defaultKey = preferredAsn && asnId ? ASN_CHOICE : SINGLE_CHOICE;
  const requestedKey = choiceKey ?? defaultKey;
  const isAsn = requestedKey === ASN_CHOICE && !!asnId;
  const cidrChoice =
    cidrChoices.find((c) => c.key === requestedKey) ?? cidrChoices[0];
  const selectedKey = isAsn ? ASN_CHOICE : cidrChoice.key;

  const resolveQuery = useQuery({
    queryKey: ["asn-resolve", asnId],
    queryFn: () => asnApi.resolve(asnId ?? ""),
    enabled: open && isAsn,
    retry: false,
    staleTime: RESOLVE_STALE_MS,
    refetchOnWindowFocus: false,
  });
  const resolved = isAsn ? resolveQuery.data : undefined;
  const resolveError = isAsn ? resolveQuery.error : null;
  const asnRejected =
    resolveError instanceof ApiError && resolveError.code === "asn_invalid";

  useEffect(() => {
    if (!resolveQuery.data) return;
    asnStorage.put(resolveQuery.data);
    storeAsnView(resolveQuery.data);
  }, [resolveQuery.data, storeAsnView]);

  const defaultSetId =
    sets.find((s) => s.enabled)?.id ?? sets[0]?.id ?? CREATE_SET_SENTINEL;
  const selectedSetId =
    setChoice &&
    (setChoice.id === CREATE_SET_SENTINEL ||
      sets.some((s) => s.id === setChoice.id))
      ? setChoice.id
      : defaultSetId;
  const creatingSet = selectedSetId === CREATE_SET_SENTINEL;
  const newSetName = creatingSet ? (setChoice?.name ?? "") : "";
  const selectedSet = sets.find((s) => s.id === selectedSetId);
  const setLabel = creatingSet
    ? newSetName.trim()
    : (selectedSet?.name ?? selectedSetId);
  const asnInSet = !!asnId && !!selectedSet?.targets.asns?.includes(asnId);

  const target: IpTarget =
    isAsn && asnId ? { kind: "asn", id: asnId } : cidrChoice.target;

  const submitBlocked =
    submitting ||
    !setLabel ||
    (isAsn && (resolveQuery.isFetching || asnInSet || asnRejected));

  const choose = (key: string) => {
    setChoiceKey(key);
    setSubmitError("");
  };

  const pickOrigin = (id: string) => {
    setPickedAsn(id);
    choose(ASN_CHOICE);
  };

  const submit = async () => {
    if (submitBlocked) return;
    setSubmitting(true);
    setSubmitError("");
    try {
      await onSubmit({
        target,
        setId: selectedSetId,
        newSetName: creatingSet ? newSetName : undefined,
        setLabel,
      });
    } catch (e) {
      setSubmitError(
        t("connections.addIp.addFailed", {
          target: ipTargetLabel(target),
          error: localizeApiError(e),
        }),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const enrich = async () => {
    setIpInfoBusy(true);
    setIpInfoError("");
    try {
      setIpInfo(await fetchIpInfo(ip));
    } catch (e) {
      setIpInfoError(localizeApiError(e));
    } finally {
      setIpInfoBusy(false);
    }
  };

  const addHostname = () => {
    if (!ipInfo?.hostname || !onAddHostname) return;
    onAddHostname(ipInfo.hostname);
    onClose();
  };

  const asnTitle = asnId ? asnLabel(asnId, origin?.name || resolved?.name) : "";

  return (
    <B4Dialog
      title={t("connections.addIp.title")}
      icon={<IpIcon />}
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>{t("core.cancel")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            onClick={() => void submit()}
            variant="contained"
            startIcon={
              submitting ? <CircularProgress size={16} color="inherit" /> : <AddIcon />
            }
            disabled={submitBlocked}
          >
            {t("connections.addIp.submit", { target: ipTargetLabel(target) })}
          </Button>
        </>
      }
    >
      <Stack spacing={2}>
        <Box
          sx={{
            p: 1.5,
            bgcolor: colors.background.dark,
            border: `1px solid ${colors.border.default}`,
            borderRadius: radius.sm,
          }}
        >
          <Stack direction="row" alignItems="flex-start" gap={1.5}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography sx={{ ...typography.recipes.metricLabel, mb: 0.5 }}>
                {t("connections.addIp.address")}
              </Typography>
              <Typography
                sx={{
                  fontFamily: typography.recipes.monoSmall.fontFamily,
                  fontSize: typography.sizes.lg,
                  color: colors.text.primary,
                  overflowWrap: "anywhere",
                }}
              >
                {ip}
              </Typography>
              <NetworkSummary
                loading={lookupQuery.isFetching}
                prefix={lookup ? coveringPrefix(lookup.prefix, ip) : null}
                origins={lookup?.asns}
              />
            </Box>
            {ipInfoToken && !ipInfo && (
              <Button
                variant="outlined"
                size="small"
                startIcon={
                  ipInfoBusy ? <CircularProgress size={14} color="inherit" /> : <IpInfoIcon />
                }
                onClick={() => void enrich()}
                disabled={ipInfoBusy}
                sx={{ flexShrink: 0 }}
              >
                {t("connections.addIp.enrichWithIpInfo")}
              </Button>
            )}
          </Stack>
          {ipInfo && (
            <IpInfoSummary
              info={ipInfo}
              onAddHostname={onAddHostname ? addHostname : undefined}
            />
          )}
          {ipInfoError && (
            <B4Alert severity="error" noWrapper sx={{ mt: 1 }}>
              {ipInfoError}
            </B4Alert>
          )}
        </Box>

        {lookupQuery.isError && !lookupQuery.isFetching && (
          <B4Alert
            severity="warning"
            noWrapper
            action={
              <Button
                color="inherit"
                size="small"
                startIcon={<RefreshIcon />}
                onClick={() => void lookupQuery.refetch()}
              >
                {t("connections.addIp.retry")}
              </Button>
            }
          >
            {t("connections.addIp.lookupFailed", {
              ip,
              error: localizeApiError(lookupQuery.error),
            })}
          </B4Alert>
        )}

        {sets.length > 0 ? (
          <SetSelector
            sets={sets}
            value={selectedSetId}
            onChange={(id, name) => {
              setSetChoice({ id, name: name ?? "" });
              setSubmitError("");
            }}
          />
        ) : (
          <B4TextField
            label={t("core.setName")}
            value={newSetName}
            onChange={(e) =>
              setSetChoice({ id: CREATE_SET_SENTINEL, name: e.target.value })
            }
            helperText={t("connections.addIp.noSets")}
          />
        )}

        <Box>
          <Typography sx={{ ...typography.recipes.metricLabel, mb: 1 }}>
            {t("connections.addIp.whatToAdd")}
          </Typography>
          <List disablePadding>
            {cidrChoices.map((choice) => (
              <ListItem key={choice.key} disablePadding sx={{ mb: 0.5 }}>
                <ListItemButton
                  onClick={() => choose(choice.key)}
                  selected={selectedKey === choice.key}
                  sx={choiceButtonSx}
                >
                  <ListItemIcon>
                    <Radio checked={selectedKey === choice.key} sx={radioSx} />
                  </ListItemIcon>
                  <ListItemText
                    primary={choice.target.label}
                    secondary={choice.hint}
                    slotProps={{
                      primary: {
                        sx: { fontFamily: typography.recipes.monoSmall.fontFamily, overflowWrap: "anywhere" },
                      },
                    }}
                  />
                </ListItemButton>
              </ListItem>
            ))}
          </List>

          {asnId && (
            <Box
              sx={{
                border: `1px solid ${isAsn ? colors.border.strong : colors.border.light}`,
                borderRadius: 1,
              }}
            >
              <ListItemButton
                onClick={() => choose(ASN_CHOICE)}
                selected={isAsn}
                disabled={asnInSet && !isAsn}
                sx={choiceButtonSx}
              >
                <ListItemIcon>
                  <Radio checked={isAsn} sx={radioSx} />
                </ListItemIcon>
                <ListItemText
                  primary={asnTitle}
                  secondary={
                    asnInSet
                      ? t("connections.addIp.asnInSet", {
                          asn: formatAsn(asnId),
                          set: setLabel,
                        })
                      : t("connections.addIp.wholeNetwork")
                  }
                  slotProps={{
                    primary: { sx: { fontWeight: typography.weights.semibold } },
                    secondary: asnInSet
                      ? { sx: { color: colors.state.warning } }
                      : undefined,
                  }}
                />
              </ListItemButton>

              {origins.length > 1 && (
                <Box sx={{ px: 2, pb: 1.5 }}>
                  <Typography
                    variant="caption"
                    component="div"
                    sx={{ color: colors.text.secondary, mb: 0.75 }}
                  >
                    {t("connections.addIp.severalOrigins", { ip })}
                  </Typography>
                  <Stack direction="row" gap={0.75} flexWrap="wrap">
                    {origins.map((o) => (
                      <B4Badge
                        key={o.id}
                        label={asnLabel(o.id, o.name)}
                        color={o.id === asnId ? "secondary" : "default"}
                        variant={o.id === asnId ? "filled" : "outlined"}
                        onClick={() => pickOrigin(o.id)}
                      />
                    ))}
                  </Stack>
                </Box>
              )}

              {isAsn && (
                <AsnDetails
                  asn={formatAsn(asnId)}
                  resolving={resolveQuery.isFetching}
                  view={resolved}
                  error={resolveError}
                  rejected={asnRejected}
                  onRetry={() => void resolveQuery.refetch()}
                />
              )}
            </Box>
          )}
        </Box>

        {submitError && (
          <B4Alert severity="error" noWrapper>
            {submitError}
          </B4Alert>
        )}
      </Stack>
    </B4Dialog>
  );
};

interface NetworkSummaryProps {
  loading: boolean;
  prefix: string | null;
  origins?: AsnLookupOrigin[];
}

const NetworkSummary = ({ loading, prefix, origins }: NetworkSummaryProps) => {
  const { t } = useTranslation();
  if (loading) {
    return (
      <Stack direction="row" alignItems="center" gap={1} sx={{ mt: 1 }}>
        <CircularProgress size={14} sx={{ color: colors.secondary }} />
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("connections.addIp.lookupLoading")}
        </Typography>
      </Stack>
    );
  }
  if (!origins) return null;
  if (origins.length === 0) {
    return (
      <Typography
        variant="caption"
        component="div"
        sx={{ mt: 1, color: colors.text.secondary }}
      >
        {t("connections.addIp.lookupNone")}
      </Typography>
    );
  }
  return (
    <Stack spacing={0.25} sx={{ mt: 1 }}>
      {prefix && (
        <SummaryLine label={t("connections.addIp.prefix")} value={prefix} mono />
      )}
      <SummaryLine
        label={t("connections.addIp.announcedBy")}
        value={origins.map((o) => asnLabel(o.id, o.name)).join(", ")}
      />
    </Stack>
  );
};

interface SummaryLineProps {
  label: string;
  value: string;
  mono?: boolean;
}

const SummaryLine = ({ label, value, mono }: SummaryLineProps) => (
  <Typography variant="body2" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
    <Box component="span" sx={{ color: colors.text.disabled, mr: 0.75 }}>
      {label}
    </Box>
    <Box
      component="span"
      sx={{
        color: colors.text.primary,
        fontFamily: mono ? typography.recipes.monoSmall.fontFamily : undefined,
      }}
    >
      {value}
    </Box>
  </Typography>
);

interface IpInfoSummaryProps {
  info: IpInfo;
  onAddHostname?: () => void;
}

const IpInfoSummary = ({ info, onAddHostname }: IpInfoSummaryProps) => {
  const { t } = useTranslation();
  const location = ipInfoLocation(info);
  return (
    <Stack
      direction="row"
      alignItems="center"
      gap={1.5}
      sx={{ mt: 1.5, pt: 1.5, borderTop: `1px solid ${colors.border.light}` }}
    >
      <Stack spacing={0.25} sx={{ flex: 1, minWidth: 0 }}>
        {info.org && <SummaryLine label={t("connections.addIp.org")} value={info.org} />}
        {info.hostname && (
          <SummaryLine label={t("connections.addIp.hostname")} value={info.hostname} mono />
        )}
        {location && (
          <SummaryLine label={t("connections.addIp.location")} value={location} />
        )}
        {!info.org && !info.hostname && !location && (
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("connections.addIp.ipInfoEmpty")}
          </Typography>
        )}
      </Stack>
      {info.hostname && onAddHostname && (
        <Button size="small" onClick={onAddHostname} sx={{ flexShrink: 0 }}>
          {t("core.addHostname")}
        </Button>
      )}
    </Stack>
  );
};

interface AsnDetailsProps {
  asn: string;
  resolving: boolean;
  view?: AsnView;
  error: unknown;
  rejected: boolean;
  onRetry: () => void;
}

const AsnDetails = ({
  asn,
  resolving,
  view,
  error,
  rejected,
  onRetry,
}: AsnDetailsProps) => {
  const { t } = useTranslation();
  if (resolving) {
    return (
      <Stack direction="row" alignItems="center" gap={1} sx={{ px: 2, pb: 1.5 }}>
        <CircularProgress size={14} sx={{ color: colors.secondary }} />
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("connections.addIp.asnResolving", { asn })}
        </Typography>
      </Stack>
    );
  }
  if (error) {
    return (
      <Box sx={{ px: 2, pb: 1.5 }}>
        <B4Alert
          severity={rejected ? "error" : "warning"}
          noWrapper
          action={
            rejected ? undefined : (
              <Button
                color="inherit"
                size="small"
                startIcon={<RefreshIcon />}
                onClick={onRetry}
              >
                {t("connections.addIp.retry")}
              </Button>
            )
          }
        >
          {localizeApiError(error)}
          {!rejected && (
            <Typography variant="caption" component="div" sx={{ mt: 0.5, opacity: 0.85 }}>
              {t("connections.addIp.asnAddAnyway")}
            </Typography>
          )}
        </B4Alert>
      </Box>
    );
  }
  if (!view) return null;
  return (
    <Box sx={{ px: 2, pb: 1.5 }}>
      <AsnFacts view={view} />
      {view.last_error && (
        <Typography
          variant="caption"
          component="div"
          sx={{ mt: 0.5, color: colors.text.secondary, overflowWrap: "anywhere" }}
        >
          {t("core.asn.lastError", { error: view.last_error })}
        </Typography>
      )}
      <AsnLargeNetworkAlert view={view} sx={{ mt: 1 }} />
    </Box>
  );
};

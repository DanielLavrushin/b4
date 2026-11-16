import { useEffect, useState, useSyncExternalStore } from "react";
import { Box, Button, Tooltip, Typography } from "@mui/material";
import { useQueries } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { asnApi } from "@api/asn";
import { AsnLookup } from "@models/asn";
import { ProbeAddress } from "@models/discovery";
import { asnLabel, asnStorage } from "@utils";

const LOOKUP_STALE_MS = 5 * 60 * 1000;

interface ProbeAddressesProps {
  addresses?: ProbeAddress[];
  site?: string;
}

const lookupLabel = (lookup: AsnLookup | undefined): string | null => {
  if (!lookup) return null;
  return lookup.asns.map((o) => asnLabel(o.id, o.name)).join(", ");
};

export const ProbeAddresses = ({ addresses, site }: ProbeAddressesProps) => {
  const { t } = useTranslation();
  const labels = useSyncExternalStore(asnStorage.subscribe, asnStorage.getLabels);
  const [lookUp, setLookUp] = useState(false);

  useEffect(() => {
    void asnStorage.init();
  }, []);

  const list = addresses ?? [];
  const known = new Map(list.map((a) => [a.ip, labels.find(a.ip)?.name ?? null]));
  const unknown = list.map((a) => a.ip).filter((ip) => !known.get(ip));

  const queries = useQueries({
    queries: unknown.map((ip) => ({
      queryKey: ["asn-lookup", ip],
      queryFn: () => asnApi.lookup(ip),
      enabled: lookUp,
      retry: false,
      staleTime: LOOKUP_STALE_MS,
      refetchOnWindowFocus: false,
    })),
  });
  const looked = new Map(unknown.map((ip, i) => [ip, queries[i]]));

  if (list.length === 0) return null;

  const owner = (ip: string): string | null => {
    const offline = known.get(ip);
    if (offline) return offline;
    const q = looked.get(ip);
    if (!q) return null;
    if (q.isFetching) return t("discovery.addresses.lookingUp");
    if (q.isError) return t("discovery.addresses.lookupFailed");
    if (q.data) return lookupLabel(q.data) || t("discovery.addresses.noAsn");
    return null;
  };
  const pending = unknown.some((ip) => !looked.get(ip)?.data);

  return (
    <Box
      sx={{
        mt: 0.5,
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: 0.25,
        minWidth: 0,
        maxWidth: "100%",
      }}
    >
      {list.map((a) => {
        const label = owner(a.ip);
        return (
          <Tooltip
            key={a.ip}
            placement="top-start"
            title={t(
              a.loaded
                ? "discovery.addresses.loaded"
                : "discovery.addresses.notLoaded",
              { site },
            )}
          >
            <Typography
              variant="caption"
              component="div"
              sx={{
                display: "flex",
                flexWrap: "wrap",
                alignItems: "center",
                columnGap: 0.75,
                minWidth: 0,
                maxWidth: "100%",
                color: a.loaded ? colors.text.secondary : colors.text.disabled,
              }}
            >
              <Box
                component="span"
                sx={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 0.5,
                  whiteSpace: "nowrap",
                  fontFamily: typography.recipes.monoSmall.fontFamily,
                }}
              >
                {a.ip}
                {a.loaded && (
                  <CheckIcon
                    sx={{ fontSize: 14, color: colors.state.success }}
                  />
                )}
              </Box>
              {label && (
                <Box
                  component="span"
                  sx={{ minWidth: 0, whiteSpace: "normal", overflowWrap: "anywhere" }}
                >
                  {label}
                </Box>
              )}
            </Typography>
          </Tooltip>
        );
      })}
      {pending && !lookUp && (
        <Button
          size="small"
          onClick={() => setLookUp(true)}
          sx={{
            textTransform: "none",
            color: colors.text.secondary,
            px: 0.5,
            ml: -0.5,
            minWidth: 0,
            fontSize: "0.75rem",
          }}
        >
          {t("discovery.addresses.lookUp")}
        </Button>
      )}
    </Box>
  );
};

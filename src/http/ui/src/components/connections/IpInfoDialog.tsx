import { useEffect, useState } from "react";
import {
  Button,
  Typography,
  Box,
  CircularProgress,
  Stack,
} from "@mui/material";
import { InfoIcon, AddIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { B4Badge } from "@common/B4Badge";
import { B4Alert } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { localizeApiError } from "@utils";
import { IpInfo, fetchIpInfo, ipInfoLocation } from "./ipinfo";

interface IpInfoModalProps {
  open: boolean;
  ip: string;
  token: string;
  onClose: () => void;
  onAddHostname?: (hostname: string) => void;
}

export const IpInfoModal = ({
  open,
  ip,
  token,
  onClose,
  onAddHostname,
}: IpInfoModalProps) => {
  const { t } = useTranslation();
  const [ipInfo, setIpInfo] = useState<IpInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !ip || !token) return;
    let active = true;
    setLoading(true);
    setError(null);
    setIpInfo(null);
    fetchIpInfo(ip)
      .then((data) => {
        if (active) setIpInfo(data);
      })
      .catch((err: unknown) => {
        if (active) setError(localizeApiError(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [open, ip, token]);

  const handleAddHostname = () => {
    if (ipInfo?.hostname && onAddHostname) {
      onAddHostname(ipInfo.hostname);
      onClose();
    }
  };

  return (
    <B4Dialog
      title={t("connections.ipInfo.title")}
      icon={<InfoIcon />}
      open={open}
      onClose={onClose}
      actions={
        <>
          {ipInfo?.hostname && onAddHostname && (
            <Button
              onClick={handleAddHostname}
              variant="contained"
              startIcon={<AddIcon />}
            >
              {t("core.addHostname")}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          <Button onClick={onClose} variant="outlined">
            {t("core.close")}
          </Button>
        </>
      }
    >
      <>
        {loading && (
          <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
            <CircularProgress />
          </Box>
        )}

        {error && (
          <B4Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </B4Alert>
        )}

        {ipInfo && !loading && (
          <Stack spacing={2}>
            {ipInfo.org && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.organization")}
                </Typography>
                <Typography variant="body1">
                  <a
                    href={`https://ipinfo.io/${encodeURIComponent(ipInfo.org.split(" ")[0])}`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {ipInfo.org}
                  </a>
                </Typography>
              </Box>
            )}

            {ipInfo.hostname && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.hostname")}
                </Typography>
                <Typography variant="body1" fontFamily="monospace">
                  <B4Badge label={ipInfo.hostname} color="secondary" />
                </Typography>
              </Box>
            )}

            <Box>
              <Typography variant="caption" color="text.secondary">
                {t("connections.ipInfo.ipAddress")}
              </Typography>
              <Typography variant="body1" fontFamily="monospace">
                <a
                  href={`https://ipinfo.io/${encodeURIComponent(ipInfo.ip)}`}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {ipInfo.ip}
                </a>
              </Typography>
            </Box>

            {(ipInfo.city || ipInfo.region || ipInfo.country) && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.location")}
                </Typography>
                <Typography variant="body1">{ipInfoLocation(ipInfo)}</Typography>
              </Box>
            )}

            {ipInfo.loc && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.coordinates")}
                </Typography>
                <Typography variant="body1" fontFamily="monospace">
                  {ipInfo.loc}
                </Typography>
              </Box>
            )}

            {ipInfo.timezone && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.timezone")}
                </Typography>
                <Typography variant="body1">{ipInfo.timezone}</Typography>
              </Box>
            )}

            {ipInfo.postal && (
              <Box>
                <Typography variant="caption" color="text.secondary">
                  {t("connections.ipInfo.postalCode")}
                </Typography>
                <Typography variant="body1">{ipInfo.postal}</Typography>
              </Box>
            )}
          </Stack>
        )}
      </>
    </B4Dialog>
  );
};

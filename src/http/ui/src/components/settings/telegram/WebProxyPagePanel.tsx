import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { Box, Button, CircularProgress, Grid, Typography } from "@mui/material";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import { DeleteIcon, DownloadIcon, UploadIcon } from "@b4.icons";
import { B4Hint } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";
import {
  useDownloadWebProxyPage,
  useRemoveWebProxyPage,
  useUploadWebProxyPage,
  useWebProxyPage,
} from "@hooks/useWebProxyPage";

interface WebProxyPagePanelProps {
  enabled: boolean;
  hostname: string;
}

export const WebProxyPagePanel = ({
  enabled,
  hostname,
}: WebProxyPagePanelProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const page = useWebProxyPage(enabled);
  const upload = useUploadWebProxyPage();
  const download = useDownloadWebProxyPage();
  const remove = useRemoveWebProxyPage();
  const custom = page.data?.custom ?? false;
  const busy = upload.isPending || download.isPending || remove.isPending;

  const onUpload = (file: File) => {
    upload.mutate(file, {
      onSuccess: () => showSuccess(t("settings.MTProto.webProxyPageUploaded")),
      onError: (e) => showError(describeApiError(e)),
    });
  };
  const onDownload = () => {
    download.mutate(undefined, {
      onError: (e) => showError(describeApiError(e)),
    });
  };
  const onRemove = () => {
    remove.mutate(undefined, {
      onSuccess: () => showSuccess(t("settings.MTProto.webProxyPageRemoved")),
      onError: (e) => showError(describeApiError(e)),
    });
  };

  return (
    <Box sx={{ mt: 2 }}>
      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        {t("settings.MTProto.webProxyPageTitle")}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        {t("settings.MTProto.webProxyPageDesc")}
      </Typography>
      <Typography variant="body2" sx={{ mb: 1 }}>
        {custom
          ? t("settings.MTProto.webProxyPageCustom", {
              size: page.data?.size ?? 0,
            })
          : t("settings.MTProto.webProxyPageBuiltin")}
      </Typography>
      <Box
        sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}
      >
        <Button
          variant="outlined"
          size="small"
          startIcon={
            upload.isPending ? <CircularProgress size={16} /> : <UploadIcon />
          }
          onClick={() => fileInputRef.current?.click()}
          disabled={busy}
        >
          {upload.isPending ? t("core.uploading") : t("core.upload")}
        </Button>
        <Button
          variant="outlined"
          size="small"
          startIcon={
            download.isPending ? (
              <CircularProgress size={16} />
            ) : (
              <DownloadIcon />
            )
          }
          onClick={onDownload}
          disabled={!custom || busy}
        >
          {t("core.download")}
        </Button>
        <Button
          variant="outlined"
          size="small"
          color="error"
          startIcon={
            remove.isPending ? <CircularProgress size={16} /> : <DeleteIcon />
          }
          onClick={onRemove}
          disabled={!custom || busy}
        >
          {t("settings.MTProto.webProxyPageRemove")}
        </Button>
        {hostname && (
          <Button
            variant="text"
            size="small"
            endIcon={<OpenInNewIcon />}
            component="a"
            href={`https://${hostname}/`}
            target="_blank"
            rel="noreferrer"
          >
            {t("settings.MTProto.webProxyPageOpen")}
          </Button>
        )}
        <input
          ref={fileInputRef}
          type="file"
          accept=".html,.htm,text/html"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) onUpload(file);
            e.target.value = "";
          }}
        />
      </Box>
      <Grid container sx={{ mt: 1 }}>
        <B4Hint>{t("settings.MTProto.webProxyPageHint")}</B4Hint>
      </Grid>
    </Box>
  );
};

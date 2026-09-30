import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, Stack, Typography } from "@mui/material";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import { SniIcon } from "@b4.icons";
import { B4IntegrationCard } from "@b4.elements";
import { B4Config, MTProtoConfig } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { MTProtoBridgeCard } from "./MTProtoBridge";
import { MTProtoSecrets } from "./MTProtoSecrets";
import { MTProtoUpstreamCard } from "./MTProtoUpstream";
import { ProxyCard } from "./ProxyCard";
import { ShareDialog } from "./ShareDialog";
import { WebCarrierCard } from "./WebCarrierCard";
import type { ShareTarget } from "./share";

interface MTProtoSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const MTProtoSettings = ({
  config,
  savedBridgeEnabled,
  savedMtproto,
  onChange,
}: MTProtoSettingsProps & {
  savedBridgeEnabled: boolean;
  savedMtproto?: MTProtoConfig;
}) => {
  const [share, setShare] = useState<ShareTarget | null>(null);

  const enabled = config.system.mtproto?.enabled ?? false;

  const openShare = (secretValue: string) => {
    const secrets = config.system.mtproto?.secrets ?? [];
    const entry = secrets.find((s) => s.secret === secretValue);
    setShare({ name: entry?.name || entry?.id || "", secret: secretValue });
  };

  return (
    <>
      <Stack spacing={2}>
        <MTProtoBridgeCard
          config={config}
          savedEnabled={savedBridgeEnabled}
          onChange={onChange}
        />
        <ProxyCard config={config} onChange={onChange} />
        {enabled && (
          <>
            <AccessCard
              config={config}
              onChange={onChange}
              onShare={openShare}
            />
            <WebCarrierCard config={config} onChange={onChange} />
          </>
        )}
        <MTProtoUpstreamCard config={config} onChange={onChange} />
        <CreditLine />
      </Stack>
      {enabled && (
        <ShareDialog
          config={config}
          savedMtproto={savedMtproto}
          target={share}
          onClose={() => setShare(null)}
        />
      )}
    </>
  );
};

const AccessCard = ({
  config,
  onChange,
  onShare,
}: MTProtoSettingsProps & { onShare: (secret: string) => void }) => {
  const { t } = useTranslation();

  return (
    <B4IntegrationCard
      icon={<SniIcon />}
      title={t("settings.MTProto.secretsTitle")}
      description={t("settings.MTProto.secretsDesc")}
    >
      <MTProtoSecrets config={config} onChange={onChange} onShare={onShare} />
    </B4IntegrationCard>
  );
};

const CreditLine = () => {
  const { t } = useTranslation();
  return (
    <Typography variant="caption" sx={{ px: 0.5 }}>
      {t("settings.MTProto.credit")}{" "}
      <Link
        href="https://github.com/Flowseal/tg-ws-proxy"
        target="_blank"
        rel="noreferrer"
        sx={{ display: "inline-flex", alignItems: "center", gap: 0.25 }}
      >
        tg-ws-proxy
        <OpenInNewIcon sx={{ fontSize: 12 }} />
      </Link>
    </Typography>
  );
};

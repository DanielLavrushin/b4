import type { ReactNode } from "react";
import { Grid, Stack } from "@mui/material";
import { B4TabPanel } from "@b4.elements";
import { spacing } from "@design";
import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { CORE_SECTIONS, CoreSectionId, coreSectionIndex } from "./coreSections";
import { LoggingSettings, ServiceSettings } from "./Core";
import { DevicesSettings } from "./Devices";
import { DnsSettings } from "./Dns";
import {
  IpVersionSettings,
  PacketEngineSettings,
  useEngineStatus,
} from "./Engine";
import {
  DscpSettings,
  FirewallRulesSettings,
  NatMasqueradeSettings,
} from "./Firewall";
import { IPHealthSettings } from "./IPHealth";
import { MSSClampingSettings } from "./MSSClamping";
import { QueueSettings } from "./Queue";
import { Socks5Settings } from "./Socks5";
import { WebServerSettings } from "./WebServer";

interface CoreSettingsProps {
  section: CoreSectionId;
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

type SectionProps = Omit<CoreSettingsProps, "section">;

const TwoColumns = ({ left, right }: { left: ReactNode; right: ReactNode }) => (
  <Grid container spacing={spacing.lg}>
    <Grid size={{ xs: 12, md: 6 }}>
      <Stack spacing={spacing.lg}>{left}</Stack>
    </Grid>
    <Grid size={{ xs: 12, md: 6 }}>
      <Stack spacing={spacing.lg}>{right}</Stack>
    </Grid>
  </Grid>
);

const EngineSection = (props: SectionProps) => {
  const { ipv6BypassesSets, engineFailure } = useEngineStatus();

  return (
    <TwoColumns
      left={
        <>
          <PacketEngineSettings {...props} engineFailure={engineFailure} />
          <IPHealthSettings {...props} />
        </>
      }
      right={
        <>
          <IpVersionSettings {...props} ipv6BypassesSets={ipv6BypassesSets} />
          <QueueSettings {...props} />
        </>
      }
    />
  );
};

export const CoreSettings = ({
  section,
  config,
  onChange,
}: CoreSettingsProps) => {
  const props = { config, onChange };

  const content: Record<CoreSectionId, ReactNode> = {
    service: (
      <TwoColumns
        left={<ServiceSettings {...props} />}
        right={<LoggingSettings {...props} />}
      />
    ),
    engine: <EngineSection {...props} />,
    devices: <DevicesSettings {...props} />,
    firewall: (
      <TwoColumns
        left={
          <>
            <FirewallRulesSettings {...props} />
            <MSSClampingSettings {...props} />
          </>
        }
        right={
          <>
            <NatMasqueradeSettings {...props} />
            <DscpSettings {...props} />
          </>
        }
      />
    ),
    dns: <DnsSettings {...props} />,
    web: (
      <TwoColumns
        left={<WebServerSettings {...props} />}
        right={<Socks5Settings {...props} />}
      />
    ),
  };

  const active = coreSectionIndex(section);

  return (
    <>
      {CORE_SECTIONS.map((s, index) => (
        <B4TabPanel
          key={s.id}
          value={active}
          index={index}
          idPrefix="core-section"
        >
          {content[s.id]}
        </B4TabPanel>
      ))}
    </>
  );
};

import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { CORE_SECTIONS } from "./sections";
import { SectionPanels, TwoColumns } from "./SectionPanels";
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

interface CoreSettingsProps {
  section: string | undefined;
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

type SectionProps = Omit<CoreSettingsProps, "section">;

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

  return (
    <SectionPanels
      sections={CORE_SECTIONS}
      active={section}
      idPrefix="general-section"
      content={{
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
        socks5: <Socks5Settings {...props} />,
      }}
    />
  );
};

import { B4Config } from "@models/config";
import { SettingsPropHandlerType } from "@models/settings";
import { SYSTEM_SECTIONS, SystemSectionId } from "./sections";
import { SectionPanels, TwoColumns } from "./SectionPanels";
import { BackupSettings } from "./Backup";
import { LoggingSettings, ServiceSettings } from "./Core";
import { WebServerSettings } from "./WebServer";

interface SystemSettingsProps {
  section: SystemSectionId;
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}

export const SystemSettings = ({
  section,
  config,
  onChange,
}: SystemSettingsProps) => {
  const props = { config, onChange };

  return (
    <SectionPanels
      sections={SYSTEM_SECTIONS}
      active={section}
      idPrefix="system-section"
      content={{
        service: (
          <TwoColumns
            left={<ServiceSettings {...props} />}
            right={<LoggingSettings {...props} />}
          />
        ),
        web: <WebServerSettings {...props} />,
        backup: <BackupSettings />,
      }}
    />
  );
};

import type { ReactNode } from "react";
import { Grid } from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  B4FieldMessage,
  B4NumberField,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import { spacing } from "@design";
import { SettingsPropHandlerType } from "@models/settings";

interface ExposeSwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  skipSetup: boolean;
  blocked?: string;
  note?: string;
  disabled?: boolean;
  path?: string;
}

export const ExposeSwitch = ({
  checked,
  onChange,
  skipSetup,
  blocked,
  note,
  disabled,
  path,
}: ExposeSwitchProps) => {
  const { t } = useTranslation();

  let message: ReactNode;
  if (skipSetup) {
    message = (
      <B4FieldMessage severity={checked ? "warning" : "info"}>
        {t("settings.Listener.exposeSkipped")}
      </B4FieldMessage>
    );
  } else if (!checked && blocked) {
    message = <B4FieldMessage severity="info">{blocked}</B4FieldMessage>;
  } else if (checked && note && !disabled) {
    message = <B4FieldMessage severity="info">{note}</B4FieldMessage>;
  }

  return (
    <B4Switch
      label={t("settings.Listener.expose")}
      checked={checked}
      path={path}
      onChange={onChange}
      disabled={disabled || (!checked && (skipSetup || !!blocked))}
      description={t("settings.Listener.exposeDesc")}
      message={message}
      info={[t("settings.Listener.exposeInfo"), note]
        .filter(Boolean)
        .join("\n\n")}
      learnMore="settings/security#expose-to-internet"
      aiTopic={path}
    />
  );
};

interface Listener {
  bind_address?: string;
  port?: number;
  expose?: boolean;
}

interface ListenerFieldsProps {
  path: string;
  listener?: Listener;
  defaultPort: number;
  skipSetup: boolean;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
  exposeBlocked?: string;
  exposeNote?: string;
  disabled?: boolean;
}

export const ListenerFields = ({
  path,
  listener,
  defaultPort,
  skipSetup,
  onChange,
  exposeBlocked,
  exposeNote,
  disabled,
}: ListenerFieldsProps) => {
  const { t } = useTranslation();

  return (
    <Grid container spacing={spacing.md}>
      <Grid size={{ xs: 12, md: 6 }}>
        <B4TextField
          label={t("settings.Listener.bindAddress")}
          value={listener?.bind_address || "0.0.0.0"}
          path={`${path}.bind_address`}
          onChange={(e) => onChange(`${path}.bind_address`, e.target.value)}
          placeholder="0.0.0.0"
          helperText={t("settings.Listener.bindAddressHelp")}
          disabled={disabled}
          selectOnFocus
        />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <B4NumberField
          label={t("settings.Listener.port")}
          value={listener?.port ?? defaultPort}
          path={`${path}.port`}
          onChange={(n) => onChange(`${path}.port`, n)}
          min={1}
          max={65535}
          helperText={t("settings.Listener.portHelp", { port: defaultPort })}
          disabled={disabled}
        />
      </Grid>
      <Grid size={{ xs: 12 }}>
        <ExposeSwitch
          checked={listener?.expose ?? false}
          path={`${path}.expose`}
          onChange={(checked) => onChange(`${path}.expose`, checked)}
          skipSetup={skipSetup}
          blocked={exposeBlocked}
          note={exposeNote}
          disabled={disabled}
        />
      </Grid>
    </Grid>
  );
};

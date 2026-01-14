import { LogsIcon } from "@b4.icons";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@design/components/ui/select";
import { Separator } from "@design/components/ui/separator";
import { Switch } from "@design/components/ui/switch";
import { B4Config, LogLevel } from "@models/config";

interface LoggingSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: number | boolean | string | string[],
  ) => void;
}

const LOG_LEVELS: Array<{ value: LogLevel; label: string }> = [
  { value: LogLevel.ERROR, label: "Error" },
  { value: LogLevel.INFO, label: "Info" },
  { value: LogLevel.TRACE, label: "Trace" },
  { value: LogLevel.DEBUG, label: "Debug" },
] as const;

export const LoggingSettings = ({ config, onChange }: LoggingSettingsProps) => {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <LogsIcon />
          <CardTitle>Logging Configuration</CardTitle>
        </div>
        <CardDescription>Configure logging behavior and output</CardDescription>
      </CardHeader>
      <Separator />
      <CardContent>
        <FieldGroup className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Field>
            <FieldLabel>Log Level</FieldLabel>
            <Select
              value={config.system.logging.level?.toString()}
              onValueChange={(value) =>
                onChange("system.logging.level", Number(value))
              }
            >
              <SelectTrigger>
                <SelectValue placeholder="Select log level" />
              </SelectTrigger>
              <SelectContent>
                {LOG_LEVELS.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value.toString()}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              Set the verbosity of logging output
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel>Error File Path</FieldLabel>
            <Input
              value={config.system.logging.error_file}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                onChange("system.logging.error_file", e.target.value)
              }
              placeholder="/var/log/b4/errors.log"
            />
            <FieldDescription>Full path to error log file</FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Instant Flush</FieldTitle>
              <FieldDescription>
                Flush logs immediately (may impact performance)
              </FieldDescription>
            </FieldContent>
            <Switch
              checked={config?.system?.logging?.instaflush}
              onCheckedChange={(checked: boolean) =>
                onChange("system.logging.instaflush", Boolean(checked))
              }
            />
          </Field>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldTitle>Syslog</FieldTitle>
              <FieldDescription>Enable syslog output</FieldDescription>
            </FieldContent>
            <Switch
              checked={config?.system?.logging?.syslog}
              onCheckedChange={(checked: boolean) =>
                onChange("system.logging.syslog", Boolean(checked))
              }
            />
          </Field>
        </FieldGroup>
      </CardContent>
    </Card>
  );
};

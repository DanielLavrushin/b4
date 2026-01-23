import { LogsIcon } from "@b4.icons";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@primitives/field";
import { Input } from "@primitives/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@primitives/select";
import { Separator } from "@primitives/separator";
import { Switch } from "@primitives/switch";
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
  const currentValue = config.system.logging.level?.toString();
  const currentLabel = LOG_LEVELS.find(
    (option) => option.value.toString() === currentValue,
  )?.label;

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
        <FieldGroup>
          <Field>
            <FieldLabel>Log Level</FieldLabel>
            <Select
              value={currentValue}
              onValueChange={(value) =>
                onChange("system.logging.level", Number(value))
              }
            >
              <SelectTrigger>
                <SelectValue placeholder="Select log level">
                  {currentLabel || null}
                </SelectValue>
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
            <FieldDescription>Verbosity of logging output</FieldDescription>
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

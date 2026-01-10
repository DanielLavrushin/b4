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
    value: number | boolean | string | string[]
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
    <Card className="flex flex-col">
      <CardHeader>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
            <LogsIcon />
          </div>
          <div className="flex-1">
            <CardTitle>Logging Configuration</CardTitle>
            <CardDescription className="mt-1">
              Configure logging behavior and output
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <Separator className="mb-4" />
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-4">
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
          </div>
          <div className="space-y-4">
            <Field
              orientation="horizontal"
              className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2"
            >
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
            <Field
              orientation="horizontal"
              className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2"
            >
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
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

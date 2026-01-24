import { ApiIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { Field, FieldDescription, FieldLabel } from "@primitives/field";
import { Input } from "@primitives/input";

export interface ApiSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | number) => void;
}

export const ApiSettings = ({ config, onChange }: ApiSettingsProps) => {
  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ApiIcon />
            IPINFO.IO Settings
          </CardTitle>

          <CardDescription>
            Configure your IPINFO.IO API token here.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Field>
            <FieldLabel>Token</FieldLabel>
            <Input
              value={config.system.api.ipinfo_token}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                onChange("system.api.ipinfo_token", e.target.value)
              }
              placeholder="abcd1234efgh"
            />
            <FieldDescription>
              Get the token from{" "}
              <a
                href="https://ipinfo.io/dashboard/token"
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary hover:underline"
              >
                IPINFO.IO Dashboard
              </a>
            </FieldDescription>
          </Field>
        </CardContent>
      </Card>
    </div>
  );
};

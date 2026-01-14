import { ApiIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { B4Config } from "@models/config";

export interface ApiSettingsProps {
  config: B4Config;
  onChange: (field: string, value: boolean | string | number) => void;
}

export const ApiSettings = ({ config, onChange }: ApiSettingsProps) => {
  return (
    <div className="space-y-6">
      <Alert>
        <AlertDescription>
          Here you can setup API settings for different services that can be
          used by B4.
        </AlertDescription>
      </Alert>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <ApiIcon />
              <CardTitle>IPINFO.IO Settings</CardTitle>
            </div>
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
    </div>
  );
};

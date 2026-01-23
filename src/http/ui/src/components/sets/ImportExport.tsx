import { CheckIcon, RefreshIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@primitives/alert";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@primitives/field";
import { Textarea } from "@primitives/textarea";
import { useEffect, useState } from "react";

import { Separator } from "@design/primitives/separator";
import { B4SetConfig } from "@models/config";

interface ImportExportSettingsProps {
  config: B4SetConfig;
  onImport: (importedConfig: B4SetConfig) => void;
}

export const ImportExportSettings = ({
  config,
  onImport,
}: ImportExportSettingsProps) => {
  const [jsonValue, setJsonValue] = useState("");
  const [originalJson, setOriginalJson] = useState("");
  const [validationError, setValidationError] = useState("");
  const [validationSuccess, setValidationSuccess] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);

  useEffect(() => {
    const formatted = JSON.stringify(config, null, 0);
    setJsonValue(formatted);
    setOriginalJson(formatted);
    setValidationError("");
    setValidationSuccess(false);
    setHasChanges(false);
  }, [config]);

  const handleJsonChange = (value: string) => {
    setJsonValue(value);
    setHasChanges(value !== originalJson);
    setValidationError("");
    setValidationSuccess(false);
  };

  function migrateSetConfig(set: Record<string, unknown>): B4SetConfig {
    const tcp = set.tcp as Record<string, unknown> | undefined;

    if (tcp) {
      // Migrate flat win_mode/win_values to nested win object
      if ("win_mode" in tcp && !tcp.win) {
        tcp.win = {
          mode: tcp.win_mode || "off",
          values: tcp.win_values || [0, 1460, 8192, 65535],
        };
        delete tcp.win_mode;
        delete tcp.win_values;
      }

      // Migrate flat desync fields to nested desync object
      if ("desync_mode" in tcp && !tcp.desync) {
        tcp.desync = {
          mode: tcp.desync_mode || "off",
          ttl: tcp.desync_ttl || 3,
          count: tcp.desync_count || 3,
          post_desync: tcp.post_desync || false,
        };
        delete tcp.desync_mode;
        delete tcp.desync_ttl;
        delete tcp.desync_count;
        delete tcp.post_desync;
      }

      // Ensure incoming exists
      if (!tcp.incoming) {
        tcp.incoming = {
          mode: "off",
          min: 14,
          max: 14,
          fake_ttl: 3,
          fake_count: 3,
          strategy: "badsum",
        };
      }
    }

    // Ensure fragmentation.seq_overlap_pattern exists
    const frag = set.fragmentation as Record<string, unknown> | undefined;
    if (frag) {
      if (!frag.seq_overlap_pattern) {
        frag.seq_overlap_pattern = [];
      }
      // Remove deprecated overlap field
      delete frag.overlap;
    }

    // Ensure faking.tls_mod exists
    const faking = set.faking as Record<string, unknown> | undefined;
    if (faking) {
      if (!faking.tls_mod) {
        faking.tls_mod = [];
      }
      if (!faking.payload_file) {
        faking.payload_file = "";
      }
    }

    return set as unknown as B4SetConfig;
  }

  const handleValidate = () => {
    try {
      const raw = JSON.parse(jsonValue) as Record<string, unknown>;
      const parsed = migrateSetConfig(raw);

      // Validate required fields
      if (
        !parsed.name ||
        !parsed.tcp ||
        !parsed.udp ||
        !parsed.fragmentation ||
        !parsed.faking ||
        !parsed.targets
      ) {
        setValidationError(
          "Invalid set configuration: missing required fields",
        );
        setValidationSuccess(false);
        return null;
      }

      setValidationError("");
      setValidationSuccess(true);
      return parsed;
    } catch (error) {
      setValidationError(
        error instanceof Error ? error.message : "Invalid JSON format",
      );
      setValidationSuccess(false);
      return null;
    }
  };

  const handleApply = () => {
    const validated = handleValidate();
    if (validated) {
      // Preserve the original ID
      validated.id = config.id;
      onImport(validated);
    }
  };

  const handleReset = () => {
    setJsonValue(originalJson);
    setHasChanges(false);
    setValidationError("");
    setValidationSuccess(false);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Import/Export Set configuration</CardTitle>
        <CardDescription>
          You can export the current set configuration as JSON, or import a new
          configuration by pasting valid JSON below.
        </CardDescription>
      </CardHeader>

      <Separator />

      <CardContent>
        <FieldGroup>
          <Field>
            <FieldLabel>Set Configuration JSON</FieldLabel>
            <Textarea
              value={jsonValue}
              onChange={(e) => handleJsonChange(e.target.value)}
              rows={10}
              spellCheck={false}
            />
            <FieldDescription>
              Edit directly or paste a configuration. Changes must be applied to
              take effect.
            </FieldDescription>
          </Field>

          {validationError && (
            <Field>
              <Alert variant="destructive">
                <AlertDescription>{validationError}</AlertDescription>
              </Alert>
            </Field>
          )}

          <Field orientation="horizontal" className="justify-end">
            {validationSuccess && !validationError && <CheckIcon />}

            <Button variant="outline" onClick={handleValidate}>
              Validate
            </Button>
            <Button
              variant="outline"
              onClick={handleReset}
              disabled={!hasChanges}
            >
              <RefreshIcon />
              Reset
            </Button>
            <Button onClick={handleApply} disabled={!hasChanges}>
              Apply Changes
            </Button>
          </Field>
        </FieldGroup>
      </CardContent>
    </Card>
  );
};

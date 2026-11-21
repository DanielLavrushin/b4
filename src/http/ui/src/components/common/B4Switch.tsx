import {
  FormControlLabel,
  Stack,
  Switch,
  SwitchProps,
  Typography,
  Box,
} from "@mui/material";
import { colors } from "@design";
import { B4Help, type B4HelpContent } from "./B4Help";
import { fieldMessage } from "./B4FieldMessage";
import { useChangedArrayItem, useChangedField, useChangedFields } from "@context/ChangedFieldsContext";
import { useEffect, useId } from "react";
import { useFieldLabel } from "./dialogLabels";

interface B4SwitchProps
  extends Omit<SwitchProps, "checked" | "onChange">,
    B4HelpContent {
  label?: string;
  checked: boolean;
  description?: string;
  message?: React.ReactNode;
  disabled?: boolean;
  path?: string;
  arrayItem?: string;
  onChange: (checked: boolean) => void;
}

const SWITCH_TEXT_INDENT = "54px";

export const B4Switch = ({
  label,
  checked,
  description,
  message,
  onChange,
  disabled,
  path,
  arrayItem,
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SwitchProps) => {
  const resolvedLabel = useFieldLabel(path, label);
  const fieldChanged = useChangedField(arrayItem ? undefined : path, resolvedLabel);
  const arrayItemChanged = useChangedArrayItem(
    path,
    arrayItem,
    arrayItem ? resolvedLabel : undefined,
  );
  const { registerValueLabels, scopeVersion } = useChangedFields();
  useEffect(() => {
    if (path && arrayItem) registerValueLabels(path, new Map([[arrayItem, resolvedLabel ?? ""]]));
  }, [path, arrayItem, resolvedLabel, registerValueLabels, scopeVersion]);
  const helperId = useId();
  const changed = arrayItem ? arrayItemChanged : fieldChanged;
  const labelColor = disabled
    ? colors.text.disabled
    : changed
      ? colors.secondary
      : colors.text.primary;
  const shownMessage = fieldMessage(message);
  const described = Boolean(description) || shownMessage !== null;
  const callerInput = props.slotProps?.input;

  return (
    <Box>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
        <FormControlLabel
          disabled={disabled}
          control={
            <Switch
              checked={checked}
              onChange={(e) => onChange(e.target.checked)}
              {...props}
              slotProps={{
                ...props.slotProps,
                input: {
                  ...(typeof callerInput === "object" ? callerInput : undefined),
                  "aria-describedby": described ? helperId : undefined,
                },
              }}
            />
          }
          label={
            <Stack direction="row" spacing={1} alignItems="center">
              <Typography
                sx={{
                  color: labelColor as string,
                  fontWeight: 500,
                }}
              >
                {resolvedLabel}
              </Typography>
              {changed && !disabled && (
                <Box
                  sx={{
                    width: 6,
                    height: 6,
                    borderRadius: "50%",
                    bgcolor: colors.secondary,
                    flexShrink: 0,
                  }}
                />
              )}
            </Stack>
          }
          sx={{
            alignItems: "flex-start",
            ml: 0,
            mr: 0,
            gap: "12px",
            minWidth: 0,
            "& .MuiFormControlLabel-label": {
              marginTop: "1px",
            },
          }}
        />
        <B4Help
          label={resolvedLabel}
          info={info}
          learnMore={learnMore}
          credit={credit}
          aiTopic={aiTopic}
          aiContext={aiContext}
          aiQuestion={aiQuestion}
          fieldDoc={description}
          value={checked}
          mt="-2px"
        />
      </Box>
      {described && (
        <Box id={helperId} sx={{ ml: SWITCH_TEXT_INDENT, mt: "2px" }}>
          {shownMessage ?? (
            <Typography
              variant="caption"
              sx={{
                display: "block",
                lineHeight: 1.5,
                color: disabled ? colors.text.disabled : colors.text.secondary,
              }}
            >
              {description}
            </Typography>
          )}
        </Box>
      )}
    </Box>
  );
};

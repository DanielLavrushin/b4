import {
  FormControlLabel,
  Stack,
  Switch,
  SwitchProps,
  Typography,
  Box,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";
import { useChangedArrayItem, useChangedField } from "@context/ChangedFieldsContext";

interface B4SwitchProps extends Omit<SwitchProps, "checked" | "onChange"> {
  label: string;
  checked: boolean;
  description?: string;
  disabled?: boolean;
  path?: string;
  arrayItem?: string;
  onChange: (checked: boolean) => void;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
}

export const B4Switch = ({
  label,
  checked,
  description,
  onChange,
  disabled,
  path,
  arrayItem,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SwitchProps) => {
  const fieldChanged = useChangedField(arrayItem ? undefined : path, label);
  const arrayItemChanged = useChangedArrayItem(
    path,
    arrayItem,
    arrayItem ? label : undefined,
  );
  const changed = arrayItem ? arrayItemChanged : fieldChanged;
  const labelColor = disabled
    ? colors.text.disabled
    : changed
      ? colors.secondary
      : colors.text.primary;
  const control = (
    <FormControlLabel
      disabled={disabled}
      control={
        <Switch
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          {...props}
        />
      }
      label={
        <Box>
          <Stack direction="row" spacing={1} alignItems="center">
            <Typography
              sx={{
                color: labelColor as string,
                fontWeight: 500,
              }}
            >
              {label}
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
        </Box>
      }
      sx={{
        alignItems: "flex-start",
        ml: 0,
        mr: 0,
        gap: "12px",
        "& .MuiFormControlLabel-label": {
          marginTop: "1px",
        },
      }}
    />
  );

  if (!aiTopic) return control;

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: 1,
        ...aiHoverRevealSx,
      }}
    >
      <Box sx={{ flex: 1 }}>{control}</Box>
      <B4AiExplain
        topic={aiTopic}
        fieldLabel={label}
        fieldDoc={description}
        value={checked}
        context={aiContext}
        question={aiQuestion}
      />
    </Box>
  );
};

export default B4Switch;

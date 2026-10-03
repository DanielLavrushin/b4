import {
  FormControlLabel,
  Switch,
  SwitchProps,
  Typography,
  Box,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";

interface B4SwitchProps extends Omit<SwitchProps, "checked" | "onChange"> {
  label: string;
  checked: boolean;
  description?: string;
  disabled?: boolean;
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
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SwitchProps) => {
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
          <Typography
            sx={{
              color: disabled ? colors.text.disabled : colors.text.primary,
              fontWeight: 500,
            }}
          >
            {label}
          </Typography>
          {description && (
            <Typography
              variant="caption"
              sx={{
                display: "block",
                color: colors.text.secondary,
                mt: "2px",
              }}
            >
              {description}
            </Typography>
          )}
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

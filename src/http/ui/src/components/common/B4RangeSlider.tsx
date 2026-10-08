import {
  Box,
  Slider,
  Stack,
  Typography,
  FormHelperText,
  SliderProps,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";
import { useChangedField, useChangedFields } from "@context/ChangedFieldsContext";

interface B4RangeSliderProps extends Omit<SliderProps, "onChange" | "value"> {
  label: string;
  value: [number, number];
  onChange: (value: [number, number]) => void;
  min?: number;
  max?: number;
  step?: number;
  helperText?: string;
  showValue?: boolean;
  valueSuffix?: string;
  alert?: React.ReactNode;
  disabled?: boolean;
  path?: string;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
}

export const B4RangeSlider = ({
  label,
  value,
  onChange,
  min = 0,
  max = 100,
  step = 1,
  helperText,
  showValue = true,
  valueSuffix = "",
  disabled,
  alert,
  path,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4RangeSliderProps) => {
  const changed = useChangedField(path, label);
  const { changedPaths } = useChangedFields();
  const changedPair =
    changed || (path ? changedPaths.has(`${path}_max`) : false);
  const labelColor = disabled
    ? colors.text.disabled
    : changedPair
      ? colors.secondary
      : colors.text.primary;
  const handleChange = (_event: Event, newValue: number | number[]) => {
    if (Array.isArray(newValue)) {
      onChange([newValue[0], newValue[1]]);
    }
  };

  const isRange = value[0] !== value[1];

  return (
    <Box sx={{ width: "100%", ...aiHoverRevealSx }}>
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          mb: 1,
        }}
      >
        <Stack direction="row" spacing={1} alignItems="center">
          <Typography
            variant="body2"
            sx={{
              color: labelColor as string,
              fontWeight: 500,
            }}
          >
            {label}
          </Typography>
          {changedPair && !disabled && (
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
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          {aiTopic && (
            <B4AiExplain
              topic={aiTopic}
              fieldLabel={label}
              fieldDoc={helperText}
              value={
                isRange
                  ? `${value[0]}${valueSuffix} - ${value[1]}${valueSuffix}`
                  : `${value[0]}${valueSuffix}`
              }
              context={aiContext}
              question={aiQuestion}
            />
          )}
          {showValue && (
            <Typography
              variant="body2"
              sx={{
                color: disabled ? colors.text.disabled : colors.secondary,
                fontWeight: 600,
                bgcolor: disabled
                  ? colors.background.dark
                  : colors.accent.secondary,
                px: 1.5,
                py: 0.5,
                borderRadius: 1,
                textAlign: "center",
              }}
            >
              {isRange
                ? `${value[0]}${valueSuffix} – ${value[1]}${valueSuffix}`
                : `${value[0]}${valueSuffix}`}
            </Typography>
          )}
        </Box>
      </Box>

      <Slider
        value={value}
        onChange={handleChange}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        valueLabelDisplay="auto"
        disableSwap
        sx={{
          color: colors.secondary,
          "& .MuiSlider-thumb": {
            bgcolor: colors.secondary,
            "&:hover, &.Mui-focusVisible": {
              boxShadow: `0 0 0 8px ${colors.accent.secondary}`,
            },
            "&.Mui-active": {
              boxShadow: `0 0 0 12px ${colors.accent.secondary}`,
            },
          },
          "& .MuiSlider-track": {
            bgcolor: colors.secondary,
            border: "none",
          },
          "& .MuiSlider-rail": {
            bgcolor: colors.background.dark,
            opacity: 1,
          },
          "& .MuiSlider-valueLabel": {
            bgcolor: colors.secondary,
            color: colors.background.default,
          },
          "&.Mui-disabled": {
            color: colors.text.disabled,
            "& .MuiSlider-thumb": {
              bgcolor: colors.text.disabled,
            },
            "& .MuiSlider-track": {
              bgcolor: colors.text.disabled,
            },
          },
        }}
        {...props}
      />

      {helperText && (
        <FormHelperText
          sx={{
            color: disabled ? colors.text.disabled : colors.text.secondary,
            pt: 0,
          }}
        >
          {helperText}
        </FormHelperText>
      )}
      {alert && <Box sx={{ mt: 1 }}>{alert}</Box>}
    </Box>
  );
};

export default B4RangeSlider;

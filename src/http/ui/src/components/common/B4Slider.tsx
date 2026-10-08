import {
  Box,
  Slider,
  Stack,
  TextField,
  Typography,
  FormHelperText,
  SliderProps,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";
import { useChangedField } from "@context/ChangedFieldsContext";
import { useEditableNumber } from "./useEditableNumber";

interface B4SliderProps extends Omit<SliderProps, "onChange"> {
  label: string;
  value: number;
  onChange: (value: number) => void;
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

export const B4Slider = ({
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
}: B4SliderProps) => {
  const changed = useChangedField(path, label);
  const labelColor = disabled
    ? colors.text.disabled
    : changed
      ? colors.secondary
      : colors.text.primary;
  const handleChange = (_event: Event, newValue: number | number[]) => {
    onChange(Array.isArray(newValue) ? newValue[0] : newValue);
  };
  const editor = useEditableNumber(value, onChange, min, max);

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
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          {aiTopic && (
            <B4AiExplain
              topic={aiTopic}
              fieldLabel={label}
              fieldDoc={helperText}
              value={value}
              context={aiContext}
              question={aiQuestion}
            />
          )}
          {showValue &&
            (editor.editing && !disabled ? (
              <>
              <TextField
                size="small"
                value={editor.text}
                autoFocus
                inputMode="numeric"
                onChange={(e) => editor.setText(e.target.value)}
                onBlur={() => editor.commit()}
                onKeyDown={(e) => {
                  if (e.key === "Enter") editor.commit();
                  else if (e.key === "Escape") editor.cancel();
                }}
                onFocus={(e) => e.target.select()}
                slotProps={{ htmlInput: { style: { width: `${Math.max(String(value).length, 2) + 1}ch`, padding: "4px 8px", textAlign: "center" } } }}
                sx={{
                  "& .MuiOutlinedInput-root": {
                    bgcolor: colors.accent.secondary,
                    fontSize: 14,
                    fontWeight: 600,
                    color: colors.secondary,
                    "& fieldset": { borderColor: colors.secondary },
                    "&.Mui-focused fieldset": {
                      borderColor: colors.secondary,
                    },
                  },
                }}
              />
              <Typography
                variant="body2"
                sx={{ color: colors.text.secondary, fontWeight: 600 }}
              >
                {valueSuffix}
              </Typography>
              </>
            ) : (
              <Typography
                variant="body2"
                onClick={disabled ? undefined : () => editor.start()}
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
                  cursor: disabled ? "default" : "text",
                  "&:hover": disabled
                    ? {}
                    : { bgcolor: colors.accent.secondaryHover },
                }}
              >
                {value}
                {valueSuffix}
              </Typography>
            ))}
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

export default B4Slider;

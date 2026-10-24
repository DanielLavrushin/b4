import { useRef, useState } from "react";
import {
  Box,
  ButtonBase,
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
import { useFieldLabel } from "./dialogLabels";

interface B4SliderProps extends Omit<SliderProps, "onChange"> {
  label?: string;
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
  const resolvedLabel = useFieldLabel(path, label);
  const changed = useChangedField(path, resolvedLabel);
  const labelColor = disabled
    ? colors.text.disabled
    : changed
      ? colors.secondary
      : colors.text.primary;
  const [dragValue, setDragValue] = useState<number | null>(null);
  const pendingRef = useRef<number | null>(null);
  const handleChange = (_event: Event, newValue: number | number[]) => {
    const next = Array.isArray(newValue) ? newValue[0] : newValue;
    pendingRef.current = next;
    setDragValue(next);
  };
  const handleCommit = () => {
    const next = pendingRef.current;
    pendingRef.current = null;
    setDragValue(null);
    if (next !== null && next !== value) onChange(next);
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
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          {aiTopic && (
            <B4AiExplain
              topic={aiTopic}
              fieldLabel={resolvedLabel}
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
                onChange={(e) => editor.setText(e.target.value)}
                onBlur={() => editor.commit()}
                onKeyDown={(e) => {
                  if (e.key === "Enter") editor.commit();
                  else if (e.key === "Escape") editor.cancel();
                }}
                onFocus={(e) => e.target.select()}
                slotProps={{ htmlInput: { inputMode: "numeric", style: { width: `${Math.max(String(value).length, 2) + 1}ch`, padding: "4px 8px", textAlign: "center" } } }}
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
              <ButtonBase
                disabled={disabled}
                onClick={() => editor.start()}
                aria-label={`${resolvedLabel ?? ""}: ${value}${valueSuffix}`.trim()}
                sx={{
                  px: 1.5,
                  py: 0.5,
                  borderRadius: 1,
                  bgcolor: disabled
                    ? colors.background.dark
                    : colors.accent.secondary,
                  cursor: disabled ? "default" : "text",
                  "&:hover": disabled
                    ? {}
                    : { bgcolor: colors.accent.secondaryHover },
                }}
              >
                <Typography
                  variant="body2"
                  sx={{
                    color: disabled ? colors.text.disabled : colors.secondary,
                    fontWeight: 600,
                  }}
                >
                  {value}
                  {valueSuffix}
                </Typography>
              </ButtonBase>
            ))}
        </Box>
      </Box>

      <Slider
        value={dragValue ?? value}
        onChange={handleChange}
        onChangeCommitted={handleCommit}
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

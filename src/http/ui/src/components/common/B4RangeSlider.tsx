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
import { useChangedField, useChangedFields } from "@context/ChangedFieldsContext";
import { useEditableNumber } from "./useEditableNumber";
import { useEffect, useRef } from "react";

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
function RangeValueInput({
  editor,
  digits,
  autofocus,
  onPairBlur,
}: {
  editor: {
    text: string;
    setText: (text: string) => void;
    commit: () => void;
    commitQuiet: () => void;
    cancel: () => void;
    close: () => void;
  };
  digits: number;
  autofocus?: boolean;
  onPairBlur: () => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    if (autofocus) inputRef.current?.focus();
  }, [autofocus]);
  return (
    <TextField
      size="small"
      value={editor.text}
      inputRef={inputRef}
      inputMode="numeric"
      onChange={(e) => editor.setText(e.target.value)}
      onBlur={(e) => {
        editor.commitQuiet();
        if (!(e.relatedTarget instanceof HTMLInputElement)) onPairBlur();
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") editor.commit();
        else if (e.key === "Escape") editor.cancel();
      }}
      onFocus={(e) => e.target.select()}
      slotProps={{ htmlInput: { style: { width: `${Math.max(digits, 2) + 1}ch`, padding: "4px 8px", textAlign: "center" } } }}
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
  );
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
  const isRange = value[0] !== value[1];
  const handleChange = (_event: Event, newValue: number | number[]) => {
    if (Array.isArray(newValue)) {
      onChange([newValue[0], newValue[1]]);
    }
  };
  const loEditor = useEditableNumber(value[0], (next) => {
    const hi = Math.max(next, value[1]);
    if (next !== value[0] || hi !== value[1]) onChange([next, hi]);
  }, min, max);
  const hiEditor = useEditableNumber(value[1], (next) => {
    const hi = Math.max(value[0], next);
    if (hi !== value[1]) onChange([value[0], hi]);
  }, min, max);

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
          {showValue &&
            ((loEditor.editing || hiEditor.editing) && !disabled ? (
              <Stack direction="row" spacing={0.5} alignItems="center">
                <RangeValueInput
                  editor={loEditor}
                  digits={String(value[0]).length}
                  autofocus
                  onPairBlur={() => {
                    loEditor.close();
                    hiEditor.close();
                  }}
                />
                <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                  –
                </Typography>
                <RangeValueInput
                  editor={hiEditor}
                  digits={String(value[1]).length}
                  onPairBlur={() => {
                    loEditor.close();
                    hiEditor.close();
                  }}
                />
                <Typography
                  variant="body2"
                  sx={{ color: colors.text.secondary, fontWeight: 600 }}
                >
                  {valueSuffix}
                </Typography>
              </Stack>
            ) : (
              <Typography
                variant="body2"
                onClick={disabled ? undefined : () => {
                  loEditor.start();
                  hiEditor.start();
                }}
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
                {isRange
                  ? `${value[0]}${valueSuffix} – ${value[1]}${valueSuffix}`
                  : `${value[0]}${valueSuffix}`}
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

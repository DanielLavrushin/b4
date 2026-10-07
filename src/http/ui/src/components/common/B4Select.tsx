import {
  Box,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  SelectProps,
  FormHelperText,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";
import { useChangedField } from "@context/ChangedFieldsContext";

interface B4SelectProps extends Omit<SelectProps<string | number>, "variant"> {
  label: string;
  options: { value: string | number; label: string }[];
  helperText?: string;
  path?: string;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
}

export const B4Select = ({
  label,
  options,
  helperText,
  path,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SelectProps) => {
  const changed = useChangedField(path, label);
  const fc = (
    <FormControl fullWidth size="small">
      <InputLabel
        shrink
        sx={
          changed
            ? {
                color: colors.secondary,
                "&.Mui-disabled": { color: colors.text.disabled },
              }
            : { color: colors.text.secondary }
        }
      >
        {label}
      </InputLabel>
      <Select
        {...props}
        label={label}
        displayEmpty
        renderValue={(selected) => {
          const match = options.find((o) => o.value === selected);
          return match?.label ?? String(selected);
        }}
        sx={{
          bgcolor: colors.background.dark,
          ...(changed && !props.disabled
            ? { boxShadow: `inset 3px 0 0 ${colors.secondary}` }
            : {}),
          "& .MuiOutlinedInput-notchedOutline": {
            borderColor: colors.border.default,
          },

          ...props.sx,
        }}
      >
        {options.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </Select>
      {helperText && (
        <FormHelperText sx={{ color: colors.text.secondary, ml: 0.1 }}>
          {helperText}
        </FormHelperText>
      )}
    </FormControl>
  );

  if (!aiTopic) return fc;

  const valStr =
    typeof props.value === "string" || typeof props.value === "number"
      ? props.value
      : undefined;

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: 1,
        ...aiHoverRevealSx,
      }}
    >
      <Box sx={{ flex: 1 }}>{fc}</Box>
      <B4AiExplain
        topic={aiTopic}
        fieldLabel={label}
        fieldDoc={helperText}
        value={valStr}
        context={aiContext}
        question={aiQuestion}
      />
    </Box>
  );
};

export default B4Select;

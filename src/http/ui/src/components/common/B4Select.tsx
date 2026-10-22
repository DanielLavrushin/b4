import {
  Box,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  SelectProps,
  FormHelperText,
  Stack,
  Typography,
} from "@mui/material";
import { colors } from "@design";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";

interface B4SelectOption {
  value: string | number;
  label: string;
  note?: string;
  disabled?: boolean;
}

interface B4SelectProps extends Omit<SelectProps<string | number>, "variant"> {
  label: string;
  options: B4SelectOption[];
  helperText?: string;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
}

export const B4Select = ({
  label,
  options,
  helperText,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SelectProps) => {
  const fc = (
    <FormControl fullWidth size="small">
      <InputLabel shrink sx={{ color: colors.text.secondary }}>{label}</InputLabel>
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
          "& .MuiOutlinedInput-notchedOutline": {
            borderColor: colors.border.default,
          },
          "&:hover .MuiOutlinedInput-notchedOutline": {
            borderColor: colors.border.medium,
          },
          "&.Mui-focused .MuiOutlinedInput-notchedOutline": {
            borderColor: colors.secondary,
          },

          ...props.sx,
        }}
      >
        {options.map((option) => (
          <MenuItem
            key={option.value}
            value={option.value}
            disabled={option.disabled}
            sx={
              option.note
                ? { whiteSpace: "normal", "&.Mui-disabled": { opacity: 1 } }
                : undefined
            }
          >
            {option.note ? (
              <Stack>
                <Box
                  component="span"
                  sx={
                    option.disabled
                      ? { color: colors.text.disabled }
                      : undefined
                  }
                >
                  {option.label}
                </Box>
                <Typography
                  variant="caption"
                  sx={{ color: colors.text.secondary }}
                >
                  {option.note}
                </Typography>
              </Stack>
            ) : (
              option.label
            )}
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

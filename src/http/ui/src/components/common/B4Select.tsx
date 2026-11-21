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
import { B4Help, useHelpVisible, type B4HelpContent } from "./B4Help";
import { fieldMessage } from "./B4FieldMessage";
import { useChangedField, useChangedFields } from "@context/ChangedFieldsContext";
import { useEffect } from "react";
import { useFieldLabel } from "./dialogLabels";

interface B4SelectOption {
  value: string | number;
  label: string;
  note?: string;
  disabled?: boolean;
}

interface B4SelectProps
  extends Omit<SelectProps<string | number>, "variant">,
    B4HelpContent {
  label?: string;
  options: B4SelectOption[];
  helperText?: React.ReactNode;
  message?: React.ReactNode;
  path?: string;
  changeLabel?: string;
}

export const B4Select = ({
  label,
  options,
  helperText,
  message,
  path,
  changeLabel,
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
  ...props
}: B4SelectProps) => {
  const resolvedLabel = useFieldLabel(path, label);
  const changed = useChangedField(path, changeLabel ?? resolvedLabel);
  const helpVisible = useHelpVisible({ info, learnMore, credit, aiTopic });
  const { registerValueLabels, scopeVersion } = useChangedFields();
  const optionLabels = JSON.stringify(
    options.map((o) => [String(o.value), o.label]),
  );
  useEffect(() => {
    if (!path || changeLabel !== undefined) return;
    registerValueLabels(
      path,
      new Map(JSON.parse(optionLabels) as [string, string][]),
    );
  }, [path, changeLabel, optionLabels, registerValueLabels, scopeVersion]);
  const fc = (
    <FormControl fullWidth size="small">
      <InputLabel
        shrink
        sx={{
          color: changed ? colors.secondary : colors.text.secondary,
          "&.Mui-disabled": { color: colors.text.disabled },
          "&.Mui-focused:not(.Mui-disabled)": {
            color: colors.secondary,
          },
        }}
      >
        {resolvedLabel}
      </InputLabel>
      <Select
        {...props}
        label={resolvedLabel}
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
      {fieldMessage(message) ??
        (helperText && (
          <FormHelperText sx={{ color: colors.text.secondary, ml: 0.1 }}>
            {helperText}
          </FormHelperText>
        ))}
    </FormControl>
  );

  if (!helpVisible) return fc;

  const valStr =
    typeof props.value === "string" || typeof props.value === "number"
      ? props.value
      : undefined;

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "flex-start",
        gap: 0.5,
      }}
    >
      <Box sx={{ flex: 1, minWidth: 0 }}>{fc}</Box>
      <Box sx={{ mt: "6px" }}>
        <B4Help
          label={resolvedLabel}
          info={info}
          learnMore={learnMore}
          credit={credit}
          aiTopic={aiTopic}
          aiContext={aiContext}
          aiQuestion={aiQuestion}
          fieldDoc={typeof helperText === "string" ? helperText : undefined}
          value={valStr}
        />
      </Box>
    </Box>
  );
};


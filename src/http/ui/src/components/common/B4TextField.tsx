import { Box, TextField, TextFieldProps } from "@mui/material";
import { colors } from "@design";
import { useChangedField } from "@context/ChangedFieldsContext";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";

export interface B4TextFieldProps extends Omit<TextFieldProps, "variant"> {
  helperText?: React.ReactNode;
  path?: string;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
  selectOnFocus?: boolean;
}

export const B4TextField = ({
  helperText,
  path,
  aiTopic,
  aiContext,
  aiQuestion,
  selectOnFocus,
  onFocus,
  ...props
}: B4TextFieldProps) => {
  const changed = useChangedField(path, props.label);
  const tf = (
    <TextField
      {...props}
      variant="outlined"
      size="small"
      fullWidth
      helperText={helperText}
      onFocus={(e) => {
        if (selectOnFocus) e.target.select();
        onFocus?.(e);
      }}
      sx={{
        "& .MuiOutlinedInput-root": {
          bgcolor: colors.background.dark,
          borderColor: colors.border.medium,
          ...(changed
            ? { boxShadow: `inset 3px 0 0 ${colors.secondary}` }
            : {}),
          "&:hover fieldset": {
            borderColor: colors.border.medium,
          },
          "&.Mui-focused fieldset": {
            borderColor: colors.secondary,
          },
        },
        "& input:-webkit-autofill, & input:-webkit-autofill:hover, & input:-webkit-autofill:focus": {
          WebkitBoxShadow: `0 0 0 100px ${colors.background.dark} inset`,
          WebkitTextFillColor: colors.text.primary,
          caretColor: colors.text.primary,
        },
        ...(changed
          ? {
              "& .MuiInputLabel-root:not(.Mui-disabled)": {
                color: colors.secondary,
              },
            }
          : {}),

        "& .MuiFormHelperText-root": {
          m: 0,
        },
        ...props.sx,
      }}
    />
  );

  if (!aiTopic) return tf;

  const labelStr = typeof props.label === "string" ? props.label : undefined;
  const docStr = typeof helperText === "string" ? helperText : undefined;
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
      <Box sx={{ flex: 1 }}>{tf}</Box>
      <B4AiExplain
        topic={aiTopic}
        fieldLabel={labelStr}
        fieldDoc={docStr}
        value={valStr}
        context={aiContext}
        question={aiQuestion}
      />
    </Box>
  );
};

export default B4TextField;

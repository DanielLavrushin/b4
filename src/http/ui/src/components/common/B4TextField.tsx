import { Box, TextField, TextFieldProps } from "@mui/material";
import { colors } from "@design";
import { useChangedField } from "@context/ChangedFieldsContext";
import { B4Help, useHelpVisible, type B4HelpContent } from "./B4Help";
import { fieldMessage } from "./B4FieldMessage";
import { useFieldLabel } from "./dialogLabels";

export interface B4TextFieldProps
  extends Omit<TextFieldProps, "variant">,
    B4HelpContent {
  helperText?: React.ReactNode;
  message?: React.ReactNode;
  path?: string;
  changed?: boolean;
  selectOnFocus?: boolean;
}

export const B4TextField = ({
  helperText,
  message,
  path,
  changed: markedChanged,
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
  selectOnFocus,
  onFocus,
  ...props
}: B4TextFieldProps) => {
  const resolvedLabel = useFieldLabel(path, typeof props.label === "string" ? props.label : undefined);
  const label = props.label ?? resolvedLabel;
  const pathChanged = useChangedField(path, label);
  const changed = pathChanged || !!markedChanged;
  const helpVisible = useHelpVisible({ info, learnMore, credit, aiTopic });
  const shownMessage = fieldMessage(message);
  const callerHelper = props.slotProps?.formHelperText;
  const tf = (
    <TextField
      {...props}
      label={label}
      variant="outlined"
      size="small"
      fullWidth
      helperText={shownMessage ?? helperText}
      slotProps={{
        ...props.slotProps,
        formHelperText: shownMessage
          ? {
              ...(typeof callerHelper === "object" ? callerHelper : undefined),
              component: "div",
            }
          : callerHelper,
      }}
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
          : {
              "& .MuiInputLabel-root.Mui-focused:not(.Mui-disabled)": {
                color: colors.secondary,
              },
            }),

        "& .MuiFormHelperText-root": {
          m: 0,
        },
        ...props.sx,
      }}
    />
  );

  if (!helpVisible && !aiTopic) return tf;

  const labelStr = typeof props.label === "string" ? props.label : resolvedLabel;
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
        gap: 0.5,
      }}
    >
      <Box sx={{ flex: 1, minWidth: 0 }}>{tf}</Box>
      {helpVisible && (
        <Box sx={{ mt: "6px" }}>
          <B4Help
            label={labelStr}
            info={info}
            learnMore={learnMore}
            credit={credit}
            aiTopic={aiTopic}
            aiContext={aiContext}
            aiQuestion={aiQuestion}
            fieldDoc={docStr}
            value={valStr}
          />
        </Box>
      )}
    </Box>
  );
};


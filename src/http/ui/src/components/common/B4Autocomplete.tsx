import { Autocomplete, CircularProgress, Box } from "@mui/material";
import { B4TextField } from "@b4.fields";
import { colors } from "@design";
import { B4PlusButton } from "@b4.elements";

interface SettingAutocompleteProps {
  label: string;
  value: string;
  options: string[];
  onChange: (value: string) => void;
  onSelect?: (value: string) => void;
  loading?: boolean;
  placeholder?: string;
  helperText?: string;
  disabled?: boolean;
}

const SettingAutocomplete = ({
  label,
  value,
  options,
  onChange,
  onSelect,
  loading = false,
  placeholder,
  helperText,
  disabled = false,
}: SettingAutocompleteProps) => {
  const handleAdd = () => {
    if (value.trim() && onSelect) {
      onSelect(value.trim());
      onChange("");
    }
  };

  return (
    <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
      <Autocomplete
        fullWidth
        value={value}
        inputValue={value}
        onChange={(_, newValue) => {
          if (newValue && onSelect) {
            onSelect(newValue);
          }
        }}
        onInputChange={(_, newValue, reason) => {
          if (reason === "input") {
            onChange(newValue);
          }
        }}
        options={options}
        loading={loading}
        disabled={disabled}
        freeSolo
        renderInput={(params) => (
          <B4TextField
            {...params}
            label={label}
            placeholder={placeholder}
            helperText={helperText}
            onKeyDown={(e) => {
              if ((e.key === "Enter" || e.key === "Tab") && value.trim()) {
                e.preventDefault();
                handleAdd();
              }
            }}
            slotProps={{
              ...params.slotProps,
              input: {
                ...params.slotProps.input,
                endAdornment: (
                  <>
                    {loading ? (
                      <CircularProgress color="inherit" size={20} />
                    ) : null}
                    {params.slotProps.input.endAdornment}
                  </>
                ),
                sx: {
                  bgcolor: colors.background.dark,
                  "&:hover": {
                    borderColor: colors.primary,
                  },
                },
              },
            }}
            sx={{
              "& .MuiOutlinedInput-root": {
                "& fieldset": {
                  borderColor: colors.border.default,
                },
                "&:hover fieldset": {
                  borderColor: colors.border.default,
                },
                "&.Mui-focused fieldset": {
                  borderColor: colors.secondary,
                },
              },
              "& .MuiInputLabel-root": {
                color: colors.text.secondary,
                "&.Mui-focused": {
                  color: colors.primary,
                },
              },
            }}
          />
        )}
      />
      {onSelect && (
        <B4PlusButton
          onClick={handleAdd}
          disabled={!value.trim() || disabled}
        />
      )}
    </Box>
  );
};

export default SettingAutocomplete;

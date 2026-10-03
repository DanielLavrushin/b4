import { InputAdornment, TextField } from "@mui/material";
import SearchIcon from "@mui/icons-material/Search";
import { colors } from "@design";

export const fieldSx = {
  "& .MuiInputLabel-root": { color: colors.text.secondary },
  "& .MuiInputLabel-root.Mui-focused": { color: colors.secondary },
  "& .MuiOutlinedInput-root": {
    bgcolor: colors.background.dark,
    "& .MuiOutlinedInput-notchedOutline": { borderColor: colors.border.default },
    "&:hover .MuiOutlinedInput-notchedOutline": { borderColor: colors.border.strong },
    "&.Mui-focused .MuiOutlinedInput-notchedOutline": { borderColor: colors.secondary },
  },
} as const;

interface SearchFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}

export function SearchField({ value, onChange, placeholder }: Readonly<SearchFieldProps>) {
  return (
    <TextField
      size="small"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      sx={{ ...fieldSx, flex: { sm: "1 1 360px" }, minWidth: { xs: "100%", sm: 320 }, maxWidth: { sm: 480 } }}
      slotProps={{
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <SearchIcon fontSize="small" />
            </InputAdornment>
          ),
        },
      }}
    />
  );
}

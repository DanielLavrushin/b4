import { InputAdornment, TextField } from "@mui/material";
import SearchIcon from "@mui/icons-material/Search";

interface SearchFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}

export function SearchField({ value, onChange, placeholder }: SearchFieldProps) {
  return (
    <TextField
      size="small"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      sx={{ minWidth: { xs: "100%", sm: 320 } }}
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

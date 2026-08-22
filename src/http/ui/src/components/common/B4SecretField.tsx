import { useState } from "react";
import {
  Button,
  CircularProgress,
  IconButton,
  InputAdornment,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { CopyIcon, EyeIcon, EyeOffIcon } from "@b4.icons";
import { colors } from "@design";
import { copyText } from "@utils";
import { useSnackbar } from "@context/SnackbarProvider";
import { B4TextField } from "./B4TextField";

const MANAGED_MASK = "••••••••••••••••";

export interface B4SecretFieldProps {
  label: string;
  value?: string;
  onChange?: (value: string) => void;
  managed?: boolean;
  configured?: boolean;
  placeholder?: string;
  helperText?: React.ReactNode;
  caption?: React.ReactNode;
  disabled?: boolean;
  copyable?: boolean;
  onGenerate?: () => void;
  generateLabel?: string;
  generating?: boolean;
  onSet?: () => void;
  setLabel?: string;
  onRemove?: () => void;
  removeLabel?: string;
  aiTopic?: string;
}

export const B4SecretField = ({
  label,
  value = "",
  onChange,
  managed = false,
  configured = false,
  placeholder,
  helperText,
  caption,
  disabled,
  copyable = true,
  onGenerate,
  generateLabel,
  generating,
  onSet,
  setLabel,
  onRemove,
  removeLabel,
  aiTopic,
}: B4SecretFieldProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [shown, setShown] = useState(false);

  const shownValue = managed ? (configured ? MANAGED_MASK : "") : value;
  const canReveal = !managed && Boolean(value);
  const canCopy = copyable && !managed && Boolean(value);

  const copy = async () => {
    if (await copyText(value)) {
      showSuccess(t("core.copied"));
    } else {
      showError(t("core.copyFailed"));
    }
  };

  const adornment =
    canReveal || canCopy ? (
      <InputAdornment position="end">
        {canReveal && (
          <Tooltip title={shown ? t("core.hide") : t("core.reveal")}>
            <IconButton size="small" onClick={() => setShown(!shown)}>
              {shown ? (
                <EyeOffIcon fontSize="small" />
              ) : (
                <EyeIcon fontSize="small" />
              )}
            </IconButton>
          </Tooltip>
        )}
        {canCopy && (
          <Tooltip title={t("core.copy")}>
            <IconButton
              size="small"
              onClick={() => {
                void copy();
              }}
            >
              <CopyIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
      </InputAdornment>
    ) : undefined;

  return (
    <Stack spacing={0.5}>
      <Stack direction="row" spacing={1} alignItems="flex-start">
        <B4TextField
          label={label}
          value={shownValue}
          type={managed || shown ? "text" : "password"}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
            onChange?.(e.target.value)
          }
          placeholder={placeholder}
          helperText={helperText}
          disabled={disabled}
          autoComplete="off"
          aiTopic={aiTopic}
          sx={{ flex: 1 }}
          slotProps={{
            input: {
              readOnly: managed || !onChange,
              endAdornment: adornment,
            },
          }}
        />
        {onSet && (
          <Button
            size="small"
            variant="outlined"
            sx={{ height: 40 }}
            disabled={disabled}
            onClick={onSet}
          >
            {setLabel}
          </Button>
        )}
        {onGenerate && (
          <Button
            size="small"
            variant="outlined"
            sx={{ height: 40 }}
            disabled={disabled || generating}
            startIcon={generating ? <CircularProgress size={14} /> : undefined}
            onClick={onGenerate}
          >
            {generateLabel}
          </Button>
        )}
        {onRemove && (
          <Button
            size="small"
            variant="text"
            color="error"
            sx={{ height: 40 }}
            disabled={disabled || (managed ? !configured : !value)}
            onClick={onRemove}
          >
            {removeLabel}
          </Button>
        )}
      </Stack>
      {caption && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {caption}
        </Typography>
      )}
    </Stack>
  );
};

export default B4SecretField;

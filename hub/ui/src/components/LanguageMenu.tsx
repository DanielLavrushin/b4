import { useState } from "react";
import { IconButton, Menu, MenuItem } from "@mui/material";
import TranslateIcon from "@mui/icons-material/Translate";
import { useTranslation } from "react-i18next";
import { setLanguage } from "@/i18n";

const languages = [
  { code: "en", label: "English" },
  { code: "ru", label: "Русский" },
];

export function LanguageMenu({ color = "default" }: { color?: "default" | "inherit" }) {
  const { t, i18n } = useTranslation();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  return (
    <>
      <IconButton color={color} size="small" onClick={(e) => setAnchor(e.currentTarget)} title={t("app.language")}>
        <TranslateIcon fontSize="small" />
      </IconButton>
      <Menu anchorEl={anchor} open={anchor !== null} onClose={() => setAnchor(null)}>
        {languages.map((l) => (
          <MenuItem
            key={l.code}
            selected={i18n.language === l.code}
            onClick={() => {
              setLanguage(l.code);
              setAnchor(null);
            }}
          >
            {l.label}
          </MenuItem>
        ))}
      </Menu>
    </>
  );
}

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";
import { B4Accordion } from "./B4Accordion";

interface B4AdvancedProps {
  title?: string;
  summary?: string;
  paths?: string[];
  customized?: number;
  children: React.ReactNode;
}

const touches = (changed: ReadonlySet<string>, path: string): boolean => {
  if (changed.has(path)) return true;
  const prefix = path + ".";
  for (const p of changed) if (p.startsWith(prefix)) return true;
  return false;
};

export const B4Advanced = ({
  title,
  summary,
  paths,
  customized = 0,
  children,
}: B4AdvancedProps) => {
  const { t } = useTranslation();
  const { changedPaths } = useChangedFields();
  const unsaved = Boolean(paths?.some((p) => touches(changedPaths, p)));
  const reveal = unsaved || customized > 0;
  const [open, setOpen] = useState(reveal);

  useEffect(() => {
    if (reveal) setOpen(true);
  }, [reveal]);

  return (
    <B4Accordion
      title={title ? t("help.advancedOf", { title }) : t("help.advanced")}
      status={
        customized > 0 ? t("help.customized", { count: customized }) : undefined
      }
      enabled={customized > 0}
      summary={summary}
      expanded={open}
      onChange={setOpen}
      hasChanges={unsaved}
    >
      {children}
    </B4Accordion>
  );
};

import type { ReactNode } from "react";
import { Grid, Stack } from "@mui/material";
import { B4TabPanel } from "@b4.elements";
import { spacing } from "@design";
import { SettingsSection, sectionIndex } from "./sections";

export const TwoColumns = ({
  left,
  right,
}: {
  left: ReactNode;
  right: ReactNode;
}) => (
  <Grid container spacing={spacing.lg}>
    <Grid size={{ xs: 12, md: 6 }}>
      <Stack spacing={spacing.lg}>{left}</Stack>
    </Grid>
    <Grid size={{ xs: 12, md: 6 }}>
      <Stack spacing={spacing.lg}>{right}</Stack>
    </Grid>
  </Grid>
);

interface SectionPanelsProps<Id extends string> {
  sections: SettingsSection<Id>[];
  active: string | undefined;
  idPrefix: string;
  content: Record<Id, ReactNode>;
}

export const SectionPanels = <Id extends string>({
  sections,
  active,
  idPrefix,
  content,
}: SectionPanelsProps<Id>) => {
  const activeIndex = sectionIndex(sections, active);
  return (
    <>
      {sections.map((section, index) => (
        <B4TabPanel
          key={section.id}
          value={activeIndex}
          index={index}
          idPrefix={idPrefix}
        >
          {content[section.id]}
        </B4TabPanel>
      ))}
    </>
  );
};

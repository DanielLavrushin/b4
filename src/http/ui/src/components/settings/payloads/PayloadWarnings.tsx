import { useTranslation } from "react-i18next";
import { Box, Typography } from "@mui/material";
import { B4Alert } from "@b4.elements";
import type {
  PayloadFacts,
  PayloadProtocol,
  PayloadWarning,
} from "@models/capture";
import { warningText } from "./facts";

interface PayloadWarningsProps {
  warnings: PayloadWarning[];
  protocol: PayloadProtocol;
  facts?: PayloadFacts | null;
}

export const PayloadWarnings = ({
  warnings,
  protocol,
  facts,
}: PayloadWarningsProps) => {
  const { t } = useTranslation();
  if (warnings.length === 0) return null;
  return (
    <B4Alert severity="warning" noWrapper>
      <Box component="ul" sx={{ m: 0, pl: warnings.length > 1 ? 2 : 0 }}>
        {warnings.map((code) => (
          <Typography
            key={code}
            component="li"
            variant="body2"
            sx={{ listStyle: warnings.length > 1 ? "disc" : "none" }}
          >
            {warningText(t, code, protocol, facts)}
          </Typography>
        ))}
      </Box>
    </B4Alert>
  );
};

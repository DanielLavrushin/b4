import { Box, Typography } from "@mui/material";
import { useState } from "react";
import { colors, gradients } from "../tokens";
import DecryptedText from "./DecryptedText";

export interface LogoProps {
  subtitle?: string;
}

export function Logo({ subtitle = "Bye Bye Big Bro" }: Readonly<LogoProps>) {
  const [hover, setHover] = useState(false);
  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        gap: 0,
        cursor: hover ? "none" : "default",
      }}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <Typography
        variant="h4"
        component="div"
        sx={{
          fontWeight: 800,
          color: colors.secondary,
          letterSpacing: "-0.08em",
          lineHeight: 1,
          background: gradients.logo,
          WebkitBackgroundClip: "text",
          WebkitTextFillColor: "transparent",
          backgroundClip: "text",
        }}
      >
        B<sup style={{ fontSize: "0.5em" }}>4</sup>
      </Typography>

      <Typography
        variant="caption"
        component="div"
        sx={{
          fontSize: "0.65rem",
          color: colors.text.secondary,
          opacity: 0.7,
          letterSpacing: "0.15em",
          textTransform: "uppercase",
          mt: -0.5,
        }}
      >
        <DecryptedText text={subtitle} externalHover={hover} />
      </Typography>
    </Box>
  );
}

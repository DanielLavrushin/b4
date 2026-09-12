import { Container, Stack } from "@mui/material";
import { HubBrowser } from "./Hub";

export function HubPage() {
  return (
    <Container
      maxWidth={false}
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "auto",
        py: 3,
      }}
    >
      <Stack spacing={3}>
        <HubBrowser />
      </Stack>
    </Container>
  );
}

import { Alert, AlertProps } from "@mui/material";

interface B4FieldMessageProps {
  children: React.ReactNode;
  severity?: AlertProps["severity"];
}

export const fieldMessage = (message: React.ReactNode): React.ReactNode =>
  message === undefined || message === false || message === "" ? null : message;

export const B4FieldMessage = ({
  children,
  severity = "warning",
}: B4FieldMessageProps) => (
  <Alert
    severity={severity}
    role="status"
    sx={{
      mt: 0.5,
      py: 0,
      px: 1.25,
      fontSize: 13,
      lineHeight: 1.45,
      "& .MuiAlert-icon": { py: "7px", mr: 1, fontSize: 18 },
      "& .MuiAlert-message": { py: "6px" },
    }}
  >
    {children}
  </Alert>
);

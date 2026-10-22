import { Grid, Divider } from "@mui/material";

export const B4FormHeader = ({
  label,
  ...props
}: {
  label: string;
  sx?: object;
}) => {
  return (
    <Grid size={{ xs: 12 }}>
      <Divider
        sx={{
          "& .MuiDivider-wrapper": {
            whiteSpace: "normal",
            flexShrink: 0,
            minWidth: 0,
            maxWidth: "100%",
          },
          ...props.sx,
        }}
      >
        {label.toUpperCase()}
      </Divider>
    </Grid>
  );
};

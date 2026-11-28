import { IconButton, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  ThumbDownIcon,
  ThumbDownOutlinedIcon,
  ThumbUpIcon,
  ThumbUpOutlinedIcon,
} from "@b4.icons";
import { colors } from "@design";
import { voteTooltip } from "@components/hub/text";
import { HubApplied, HubVoteKind } from "@models/hub";

const VOTE_ICON = 16;

interface VoteThumbsProps {
  applied: Pick<HubApplied, "vote" | "voted_at"> | null | undefined;
  busy: boolean;
  onVote: (kind: HubVoteKind) => void;
}

export const VoteThumbs = ({ applied, busy, onVote }: VoteThumbsProps) => {
  const { t } = useTranslation();
  const thumb = (kind: HubVoteKind) => {
    const chosen = applied?.vote === kind;
    const icon =
      kind === "works" ? (
        chosen ? (
          <ThumbUpIcon sx={{ fontSize: VOTE_ICON }} />
        ) : (
          <ThumbUpOutlinedIcon sx={{ fontSize: VOTE_ICON }} />
        )
      ) : chosen ? (
        <ThumbDownIcon sx={{ fontSize: VOTE_ICON }} />
      ) : (
        <ThumbDownOutlinedIcon sx={{ fontSize: VOTE_ICON }} />
      );
    return (
      <Tooltip key={kind} title={voteTooltip(t, applied ?? {}, kind)}>
        <span>
          <IconButton
            size="small"
            aria-label={t(kind === "works" ? "hub.card.works" : "hub.card.broken")}
            disabled={busy}
            onClick={(e) => {
              e.stopPropagation();
              onVote(kind);
            }}
            sx={{
              color: kind === "works" ? colors.state.success : colors.state.error,
              p: "4px",
            }}
          >
            {icon}
          </IconButton>
        </span>
      </Tooltip>
    );
  };
  return (
    <>
      {thumb("works")}
      {thumb("broken")}
    </>
  );
};

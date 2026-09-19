package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/spf13/cobra"
)

var moderateCmd = &cobra.Command{
	Use:   "moderate",
	Short: "Moderate shared sets and mirrors from the command line",
	Long: `Moderate shared sets and mirrors without the web console.

A set is named by its id, optionally with a version: <id> or <id>/<version>.
Without a version the newest version in the expected state is used.`,
}

func init() {
	moderateCmd.AddCommand(
		moderateListCmd,
		moderateApproveCmd,
		moderateRejectCmd,
		moderateHideCmd,
		moderateBanCmd,
		moderateMirrorsCmd,
		moderateApproveMirrorCmd,
		moderateRejectMirrorCmd,
	)
}

func withStore(run func(ctx context.Context, st *store.Store, now time.Time, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := openServices(dataDir)
		if err != nil {
			return err
		}
		defer svc.store.Close()
		return run(context.Background(), svc.store, time.Now(), args)
	}
}

var moderateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List versions waiting for moderation",
	Args:  cobra.NoArgs,
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		pending, err := st.PendingVersions(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tVERSION\tTITLE\tFAMILY\tFLAGS\tAUTHOR\tASN\tRECEIVED")
		for _, v := range pending {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", v.SetID, v.Version, v.Title, v.Family, strings.Join(v.Flags, ","), hubdata.AuthorLabel(v.UploaderHMAC), v.ASNObserved, v.CreatedAt.Format(time.RFC3339))
		}
		return tw.Flush()
	}),
}

var moderateApproveCmd = &cobra.Command{
	Use:   "approve <id>[/<version>]",
	Short: "Approve a pending version, listing it in the next catalogue",
	Args:  cobra.ExactArgs(1),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		v, err := resolveVersion(ctx, st, args[0], hubwire.SetStatusPending)
		if err != nil {
			return err
		}
		if err := st.Approve(ctx, v.SetID, v.Version, now); err != nil {
			return err
		}
		fmt.Printf("approved %s/%d\n", v.SetID, v.Version)
		return nil
	}),
}

var moderateRejectCmd = &cobra.Command{
	Use:   "reject <id>[/<version>] <reason>...",
	Short: "Reject a pending version",
	Args:  cobra.MinimumNArgs(2),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		v, err := resolveVersion(ctx, st, args[0], hubwire.SetStatusPending)
		if err != nil {
			return err
		}
		if err := st.Reject(ctx, v.SetID, v.Version, strings.Join(args[1:], " "), now); err != nil {
			return err
		}
		fmt.Printf("rejected %s/%d\n", v.SetID, v.Version)
		return nil
	}),
}

var moderateHideCmd = &cobra.Command{
	Use:   "hide <id>[/<version>] <reason>...",
	Short: "Hide a listed version from the catalogue",
	Args:  cobra.MinimumNArgs(2),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		v, err := resolveVersion(ctx, st, args[0], hubwire.SetStatusActive)
		if err != nil {
			return err
		}
		if err := st.Hide(ctx, v.SetID, v.Version, strings.Join(args[1:], " "), now); err != nil {
			return err
		}
		fmt.Printf("hidden %s/%d\n", v.SetID, v.Version)
		return nil
	}),
}

var moderateBanCmd = &cobra.Command{
	Use:   "ban <key_hmac> [reason]...",
	Short: "Ban an uploader key; its sets leave the catalogue",
	Args:  cobra.MinimumNArgs(1),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		reason := "banned by moderator"
		if len(args) > 1 {
			reason = strings.Join(args[1:], " ")
		}
		if err := st.BanKey(ctx, args[0], reason, now); err != nil {
			return err
		}
		fmt.Printf("banned %s\n", args[0])
		return nil
	}),
}

var moderateMirrorsCmd = &cobra.Command{
	Use:   "mirrors",
	Short: "List announced mirrors",
	Args:  cobra.NoArgs,
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		mirrors, err := st.Mirrors(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tURL\tVERSION\tKEY\tSTATUS\tFIRST SEEN\tLAST SEEN\tLAST CHECK\tLAST OK\tREASON")
		for _, m := range mirrors {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.URL, m.Version, hubdata.AuthorLabel(m.KeyHMAC), m.Status, stamp(m.FirstSeen), stamp(m.LastSeen), stamp(m.LastCheck), stamp(m.LastOK), m.Reason)
		}
		return tw.Flush()
	}),
}

var moderateApproveMirrorCmd = &cobra.Command{
	Use:   "approve-mirror <id>",
	Short: "Approve a mirror so healthy checks list it in the manifest",
	Args:  cobra.ExactArgs(1),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		id, err := parseMirrorID(args[0])
		if err != nil {
			return err
		}
		if err := st.SetMirrorStatus(ctx, id, store.MirrorApproved, "", now); err != nil {
			return err
		}
		fmt.Printf("approved mirror %d\n", id)
		return nil
	}),
}

var moderateRejectMirrorCmd = &cobra.Command{
	Use:   "reject-mirror <id> <reason>...",
	Short: "Reject an announced mirror",
	Args:  cobra.MinimumNArgs(2),
	RunE: withStore(func(ctx context.Context, st *store.Store, now time.Time, args []string) error {
		id, err := parseMirrorID(args[0])
		if err != nil {
			return err
		}
		if err := st.SetMirrorStatus(ctx, id, store.MirrorRejected, strings.Join(args[1:], " "), now); err != nil {
			return err
		}
		fmt.Printf("rejected mirror %d\n", id)
		return nil
	}),
}

func parseVersionRef(ref string) (string, int, error) {
	id, rest, hasVersion := strings.Cut(ref, "/")
	if !hubdata.ValidSetID(id) {
		return "", 0, fmt.Errorf("%q is not a set id", id)
	}
	if !hasVersion {
		return id, 0, nil
	}
	version, err := strconv.Atoi(rest)
	if err != nil || version <= 0 {
		return "", 0, fmt.Errorf("%q is not a version number", rest)
	}
	return id, version, nil
}

func resolveVersion(ctx context.Context, st *store.Store, ref, wantStatus string) (*store.Version, error) {
	id, version, err := parseVersionRef(ref)
	if err != nil {
		return nil, err
	}
	if version > 0 {
		return st.GetVersion(ctx, id, version)
	}
	_, versions, err := st.GetSet(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if wantStatus == "" || versions[i].Status == wantStatus {
			return &versions[i], nil
		}
	}
	return nil, fmt.Errorf("set %s has no %s version, name one as %s/<version>", id, wantStatus, id)
}

func parseMirrorID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%q is not a mirror id", raw)
	}
	return id, nil
}

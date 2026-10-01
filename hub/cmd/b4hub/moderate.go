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
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/spf13/cobra"
)

var moderateCmd = &cobra.Command{
	Use:   "moderate",
	Short: "Moderate shared sets, keys, reports and mirrors from the command line",
	Long: `Moderate shared sets, keys, reports and mirrors without the web console.

A set is named by its id, optionally with a version: <id> or <id>/<version>.
Without a version the newest version in the expected state is used.

Every change is written to the audit log and asks a running b4hub serve to
publish; the running hub picks the request up within about ten seconds.`,
}

var moderateFlags struct {
	force    bool
	withdraw bool
	create   bool
	confirm  string
	builtin  bool
	note     string
	state    string
	limit    int
	target   string
}

func init() {
	moderateCmd.AddCommand(
		moderateListCmd,
		moderateApproveCmd,
		moderateRejectCmd,
		moderateHideCmd,
		moderateRestoreCmd,
		moderateWithdrawCmd,
		moderateReinstateCmd,
		moderateDeleteCmd,
		moderateBanCmd,
		moderateKeyCmd(moderation.ActionUnban, "Lift a ban; the key's sets return at the next build"),
		moderateKeyCmd(moderation.ActionTrust, "Exempt a key from the per-key daily limits"),
		moderateKeyCmd(moderation.ActionUntrust, "Put a key back under the per-key daily limits"),
		moderateReportsCmd,
		moderateReportCmd(moderation.ActionDismiss, "Dismiss reports; they stop counting toward the automatic hide"),
		moderateReportCmd(moderation.ActionResolve, "Mark reports as handled"),
		moderateReportCmd(moderation.ActionReopen, "Reopen reports"),
		moderateMirrorsCmd,
		moderateApproveMirrorCmd,
		moderateRejectMirrorCmd,
		moderateRemoveMirrorCmd,
		moderateCheckMirrorCmd,
		moderateRevokeCmd,
		moderateEpochCmd,
		moderateStatusCmd,
		moderateBuildsCmd,
		moderateAuditCmd,
	)
	moderateApproveCmd.Flags().BoolVar(&moderateFlags.force, "force", false, "approve even when the author is banned or the set is withdrawn")
	moderateRejectCmd.Flags().BoolVar(&moderateFlags.withdraw, "withdraw", false, "also withdraw the whole set")
	moderateHideCmd.Flags().BoolVar(&moderateFlags.withdraw, "withdraw", false, "also withdraw the whole set")
	moderateDeleteCmd.Flags().StringVar(&moderateFlags.confirm, "confirm", "", "the set id again, to confirm the permanent deletion")
	moderateBanCmd.Flags().BoolVar(&moderateFlags.create, "create", false, "ban a key the hub has never seen")
	moderateReportsCmd.Flags().StringVar(&moderateFlags.state, "state", store.ReportOpen, "open, dismissed, resolved or all")
	moderateReportsCmd.Flags().IntVar(&moderateFlags.limit, "limit", 50, "rows to print")
	moderateRevokeCmd.Flags().StringVar(&moderateFlags.confirm, "confirm", "", "the key id again, to confirm the permanent revocation")
	moderateRevokeCmd.Flags().BoolVar(&moderateFlags.builtin, "allow-builtin", false, "allow revoking a key built into b4")
	moderateBuildsCmd.Flags().IntVar(&moderateFlags.limit, "limit", 20, "rows to print")
	moderateAuditCmd.Flags().IntVar(&moderateFlags.limit, "limit", 50, "rows to print")
	moderateAuditCmd.Flags().StringVar(&moderateFlags.target, "target", "", "only entries about this set id, key HMAC or mirror id")
	for _, cmd := range moderateCmd.Commands() {
		if strings.HasSuffix(cmd.Name(), "-report") {
			cmd.Flags().StringVar(&moderateFlags.note, "note", "", "a note kept with the report")
		}
	}
}

type moderateRun func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error

func withModeration(run moderateRun) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := openServices(dataDir)
		if err != nil {
			return err
		}
		defer svc.store.Close()
		return run(context.Background(), svc, svc.moderation(nil), args)
	}
}

func printEffects(items []moderation.Item) {
	for _, it := range items {
		ref := it.SetID + "/" + strconv.Itoa(it.Version)
		if !it.OK {
			fmt.Printf("%s: refused (%s)\n", ref, it.Code)
			continue
		}
		effect := "no catalogue change"
		switch {
		case it.Listed == it.ListedAfter:
		case it.ListedAfter == 0:
			effect = "the set leaves the catalogue"
		case it.Listed == 0:
			effect = "v" + strconv.Itoa(it.ListedAfter) + " is listed"
		default:
			effect = "v" + strconv.Itoa(it.ListedAfter) + " is listed instead of v" + strconv.Itoa(it.Listed)
		}
		fmt.Printf("%s: %s -> %s, %s\n", ref, it.From, it.To, effect)
	}
}

func moderateVersions(ctx context.Context, svc *services, mod *moderation.Service, action, want string, refs []string, opts moderation.Options) error {
	resolved := make([]moderation.Ref, 0, len(refs))
	for _, raw := range refs {
		v, err := resolveVersion(ctx, svc.store, raw, want)
		if err != nil {
			return err
		}
		resolved = append(resolved, moderation.Ref{SetID: v.SetID, Version: v.Version})
	}
	res, err := mod.Moderate(ctx, cliActor(), action, resolved, opts)
	printEffects(res.Items)
	if err != nil {
		return err
	}
	fmt.Println(res.Notice)
	return nil
}

var moderateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List versions waiting for moderation",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		pending, err := svc.store.PendingVersions(ctx)
		if err != nil {
			return err
		}
		banned, err := svc.store.BannedKeySet(ctx)
		if err != nil {
			return err
		}
		withheld, err := svc.store.Withheld(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tVERSION\tTITLE\tFAMILY\tFLAGS\tAUTHOR\tASN\tRECEIVED\tNOTE")
		for _, v := range pending {
			note := withheld[v.SetID]
			if banned[v.UploaderHMAC] {
				note = store.WithheldAuthorBanned
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", v.SetID, v.Version, v.Title, v.Family, strings.Join(v.Flags, ","), hubdata.AuthorLabel(v.UploaderHMAC), v.ASNObserved, v.CreatedAt.Format(time.RFC3339), note)
		}
		return tw.Flush()
	}),
}

var moderateApproveCmd = &cobra.Command{
	Use:   "approve <id>[/<version>]...",
	Short: "Approve pending versions; all or nothing",
	Args:  cobra.MinimumNArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		return moderateVersions(ctx, svc, mod, moderation.ActionApprove, hubwire.SetStatusPending, args, moderation.Options{Force: moderateFlags.force})
	}),
}

var moderateRejectCmd = &cobra.Command{
	Use:   "reject <id>[/<version>] <reason>...",
	Short: "Reject a pending version; the reason stays on the hub",
	Args:  cobra.MinimumNArgs(2),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		return moderateVersions(ctx, svc, mod, moderation.ActionReject, hubwire.SetStatusPending, args[:1], moderation.Options{Reason: strings.Join(args[1:], " "), Withdraw: moderateFlags.withdraw})
	}),
}

var moderateHideCmd = &cobra.Command{
	Use:   "hide <id>[/<version>] <reason>...",
	Short: "Hide a listed version; an older active version of the set is listed instead unless --withdraw",
	Args:  cobra.MinimumNArgs(2),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		return moderateVersions(ctx, svc, mod, moderation.ActionHide, hubwire.SetStatusActive, args[:1], moderation.Options{Reason: strings.Join(args[1:], " "), Withdraw: moderateFlags.withdraw})
	}),
}

var moderateRestoreCmd = &cobra.Command{
	Use:   "restore <id>[/<version>]...",
	Short: "Return hidden versions to the state they were hidden from and dismiss their open reports",
	Args:  cobra.MinimumNArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		return moderateVersions(ctx, svc, mod, moderation.ActionRestore, hubwire.SetStatusHidden, args, moderation.Options{})
	}),
}

var moderateWithdrawCmd = &cobra.Command{
	Use:   "withdraw <id> [reason]...",
	Short: "Take a whole set out of the catalogue; versions keep their states",
	Args:  cobra.MinimumNArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		res, err := mod.Withdraw(ctx, cliActor(), args[0], strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

var moderateReinstateCmd = &cobra.Command{
	Use:   "reinstate <id>",
	Short: "Undo a withdrawal; the set is listed as it was before",
	Args:  cobra.ExactArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		res, err := mod.Reinstate(ctx, cliActor(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

var moderateDeleteCmd = &cobra.Command{
	Use:   "delete <id> --confirm <id>",
	Short: "Delete a set with all its versions, votes and reports permanently",
	Args:  cobra.ExactArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		res, err := mod.Delete(ctx, cliActor(), args[0], moderateFlags.confirm)
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

func printImpact(impact *store.KeyImpact) {
	fmt.Printf("listed sets that leave the catalogue: %d\n", len(impact.Listed))
	for _, v := range impact.Listed {
		fmt.Printf("  %s/%d %s\n", v.SetID, v.Version, v.Title)
	}
	fmt.Printf("pending shares: %d\n", len(impact.Pending))
	fmt.Printf("votes that stop counting: %d on %d strategies\n", impact.Votes, impact.VotedSets)
	fmt.Printf("open reports that stop counting: %d\n", impact.Reports)
}

var moderateBanCmd = &cobra.Command{
	Use:   "ban <key_hmac> [reason]...",
	Short: "Ban a key: new records are refused, its sets are withheld and its votes and reports stop counting",
	Args:  cobra.MinimumNArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		key := strings.ToLower(args[0])
		if impact, err := mod.KeyImpact(ctx, key); err == nil {
			printImpact(impact)
		}
		res, err := mod.Key(ctx, cliActor(), key, moderation.ActionBan, moderation.KeyOptions{Reason: strings.Join(args[1:], " "), Create: moderateFlags.create})
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

func moderateKeyCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <key_hmac>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
			res, err := mod.Key(ctx, cliActor(), args[0], action, moderation.KeyOptions{})
			if err != nil {
				return err
			}
			fmt.Println(res.Notice)
			return nil
		}),
	}
}

var moderateReportsCmd = &cobra.Command{
	Use:   "reports",
	Short: "List reports, open ones by default",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		state := moderateFlags.state
		if state == "all" {
			state = ""
		}
		items, total, _, err := svc.store.QueryReports(ctx, store.ReportFilter{State: state, Limit: moderateFlags.limit})
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSET\tSTATE\tCOUNTS\tKEY\tASN\tRECEIVED\tREASON")
		for _, r := range items {
			fmt.Fprintf(tw, "%d\t%s/%d\t%s\t%t\t%s\t%s\t%s\t%s\n", r.ID, r.SetID, r.Version, r.State, store.Counts(r), hubdata.AuthorLabel(r.KeyHMAC), r.ASNObserved, r.ReceivedAt.Format(time.RFC3339), r.Reason)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Printf("%d of %d\n", len(items), total)
		return nil
	}),
}

func moderateReportCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + "-report <id>...",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
			ids := make([]int64, 0, len(args))
			for _, raw := range args {
				id, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || id <= 0 {
					return fmt.Errorf("%q is not a report id", raw)
				}
				ids = append(ids, id)
			}
			res, err := mod.Reports(ctx, cliActor(), ids, action, moderateFlags.note)
			if err != nil {
				return err
			}
			fmt.Println(res.Notice)
			return nil
		}),
	}
}

var moderateMirrorsCmd = &cobra.Command{
	Use:   "mirrors",
	Short: "List announced mirrors",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		mirrors, err := svc.store.Mirrors(ctx)
		if err != nil {
			return err
		}
		announced := map[string]bool{}
		if published, err := catalogue.ReadPublished(svc.layout.Public()); err == nil {
			for _, u := range published.Manifest.Mirrors {
				announced[u] = true
			}
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tURL\tVERSION\tKEY\tSTATUS\tANNOUNCED\tSERVES\tLAST ANNOUNCED\tLAST CHECK\tLAST OK\tCHECK ERROR\tREASON")
		for _, m := range mirrors {
			serves := "-"
			if m.ServedEpoch > 0 {
				serves = strconv.FormatInt(m.ServedEpoch, 10) + "/" + strconv.FormatInt(m.ServedSeq, 10)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.URL, m.Version, hubdata.AuthorLabel(m.KeyHMAC), m.Status, announced[m.URL], serves, stamp(m.LastSeen), stamp(m.LastCheck), stamp(m.LastOK), m.CheckError, m.Reason)
		}
		return tw.Flush()
	}),
}

func moderateMirror(action string) func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
	return func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		id, err := parseMirrorID(args[0])
		if err != nil {
			return err
		}
		res, _, err := mod.Mirror(ctx, cliActor(), id, action, strings.Join(args[1:], " "))
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}
}

var moderateApproveMirrorCmd = &cobra.Command{
	Use:   "approve-mirror <id>",
	Short: "Approve a mirror; it is announced once a health check passes",
	Args:  cobra.ExactArgs(1),
	RunE:  withModeration(moderateMirror(moderation.ActionApprove)),
}

var moderateRejectMirrorCmd = &cobra.Command{
	Use:   "reject-mirror <id> <reason>...",
	Short: "Reject an announced mirror",
	Args:  cobra.MinimumNArgs(2),
	RunE:  withModeration(moderateMirror(moderation.ActionReject)),
}

var moderateRemoveMirrorCmd = &cobra.Command{
	Use:   "remove-mirror <id>",
	Short: "Forget a mirror; it comes back as pending if it announces again",
	Args:  cobra.ExactArgs(1),
	RunE:  withModeration(moderateMirror(moderation.ActionRemove)),
}

var moderateCheckMirrorCmd = &cobra.Command{
	Use:   "check-mirror <id>",
	Short: "Check a mirror's health and what it serves now",
	Args:  cobra.ExactArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		id, err := parseMirrorID(args[0])
		if err != nil {
			return err
		}
		m, err := svc.store.GetMirror(ctx, id)
		if err != nil {
			return err
		}
		c := svc.builder("", nil).Mirrors.CheckOne(ctx, *m)
		if !c.OK {
			fmt.Printf("%s: failed at %s: %s (%d ms)\n", m.URL, c.Code, c.Error, c.Millis)
			return nil
		}
		fmt.Printf("%s: ok, serves %d/%d generated %s (%d ms)\n", m.URL, c.Epoch, c.Seq, c.GeneratedAt, c.Millis)
		return nil
	}),
}

var moderateRevokeCmd = &cobra.Command{
	Use:   "revoke <key_id> --confirm <key_id>",
	Short: "Revoke a hub signing key; routers never forget a revocation",
	Args:  cobra.ExactArgs(1),
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		res, err := mod.Revoke(ctx, cliActor(), args[0], moderation.RevokeOptions{Confirm: moderateFlags.confirm, AllowBuiltin: moderateFlags.builtin})
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

var moderateEpochCmd = &cobra.Command{
	Use:   "epoch",
	Short: "Start a new catalogue epoch",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		res, err := mod.NewEpoch(ctx, cliActor())
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
		return nil
	}),
}

func printBuild(label string, b *store.BuildRun) {
	if b == nil {
		fmt.Printf("%s: none\n", label)
		return
	}
	if b.OK {
		fmt.Printf("%s: %s %s, %d sets, %d ms (%s)\n", label, stamp(b.FinishedAt), b.File, b.Sets, b.DurationMs, b.Trigger)
		return
	}
	fmt.Printf("%s: %s %s (%s)\n", label, stamp(b.FinishedAt), b.Error, b.Trigger)
}

var moderateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print the publishing status",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		dirty, err := svc.store.Dirty(ctx)
		if err != nil {
			return err
		}
		ok, err := svc.store.LastBuild(ctx, true)
		if err != nil {
			return err
		}
		failed, err := svc.store.LastBuild(ctx, false)
		if err != nil {
			return err
		}
		fmt.Printf("unpublished changes: %t\n", dirty)
		printBuild("last build", ok)
		if failed != nil && (ok == nil || failed.ID > ok.ID) {
			printBuild("last failure", failed)
		}
		return nil
	}),
}

var moderateBuildsCmd = &cobra.Command{
	Use:   "builds",
	Short: "Print the build history",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		runs, _, err := svc.store.BuildRuns(ctx, store.BuildQuery{Limit: moderateFlags.limit})
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tSTARTED\tTRIGGER\tOK\tSEQ\tSETS\tADDED\tREMOVED\tUPDATED\tRESCORED\tERROR")
		for _, b := range runs {
			c := b.Changes
			fmt.Fprintf(tw, "%d\t%s\t%s\t%t\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n", b.ID, stamp(b.StartedAt), b.Trigger, b.OK, b.Seq, b.Sets, len(c.Added), len(c.Removed), len(c.Updated)+len(c.Edited), c.Rescored, b.Error)
		}
		return tw.Flush()
	}),
}

var moderateAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Print the audit log of moderation actions",
	Args:  cobra.NoArgs,
	RunE: withModeration(func(ctx context.Context, svc *services, mod *moderation.Service, args []string) error {
		q := store.AuditQuery{Limit: moderateFlags.limit, TargetID: moderateFlags.target}
		entries, _, err := svc.store.AuditLog(ctx, q)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tAT\tACTOR\tACTION\tTARGET\tREASON")
		for _, e := range entries {
			target := e.TargetID
			if e.Version > 0 {
				target += "/" + strconv.Itoa(e.Version)
			}
			actor := e.Actor
			if e.ActorRef != "" {
				actor += ":" + e.ActorRef
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", e.ID, stamp(e.At), actor, e.Action, target, e.Reason)
		}
		return tw.Flush()
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

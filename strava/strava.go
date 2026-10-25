// Package strava gives a program the commands for the athlete's own Strava
// activities -- strava login, logout, status and list -- and the --strava-config
// flag the program's other commands open a client with, the same in
// fitdash, videofx and course.
//
// The work is fitactivity/strava's; this is only what the commands share.
package strava

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	fs "github.com/wisborg/fitactivity/strava"
)

// configFlag is the flag naming the credentials file, and refreshFlag the
// one asking Strava again rather than the cache.
const (
	configFlag  = "strava-config"
	refreshFlag = "strava-refresh"
)

// Add adds the strava command beneath root, and --strava-config to every
// command, for the ones that fetch from Strava.
func Add(root *cobra.Command) *cobra.Command {
	root.PersistentFlags().String(configFlag, "", "the file holding your Strava API application's client id and secret (default: $"+fs.EnvConfig+", then ~/.config/strava.yaml)")
	root.PersistentFlags().Bool(refreshFlag, false, "ask Strava again for the activities a command fetches, rather than the answers kept for a day, and keep the new ones: for an activity edited on Strava since")
	program := root.Name()
	cmd := &cobra.Command{
		Use:   "strava",
		Short: "Log in to Strava, and list your activities there",
		Long: `strava connects ` + program + ` to your own Strava activities, through your own
Strava API application: register one at https://www.strava.com/settings/api,
with "localhost" as its Authorization Callback Domain, and put its client id
and secret in ~/.config/strava.yaml:

  strava:
    client_id: 12345
    client_secret: 0123abcd...

or in $` + fs.EnvClientID + ` and $` + fs.EnvClientSecret + `. Then "` + program + ` strava login" once.

What is fetched is kept for a day and then deleted, as Strava's API Policy
asks, and only your own activities are fetched. An activity edited on Strava
since it was fetched -- gear added, a title changed -- is read as it was until
then; --strava-refresh asks for it again, and keeps the new answer. Nothing contacts Strava
unless a command is asked to.`,
	}
	cmd.AddCommand(loginCmd(), logoutCmd(), statusCmd(), listCmd())
	root.AddCommand(cmd)
	return cmd
}

// Open is the client a command fetches with: the application from
// --strava-config, the login beside it, and the cache. A problem with the
// credentials file that does not stop it being used is said on w.
func Open(cmd *cobra.Command, w io.Writer) (*fs.Client, error) {
	path, _ := cmd.Flags().GetString(configFlag)
	creds, err := fs.LoadCredentials(path)
	if err != nil {
		return nil, err
	}
	if creds.Warning != "" {
		fmt.Fprintf(w, "%s: warning: %s\n", cmd.Root().Name(), creds.Warning)
	}
	dir, err := fs.DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	cache, err := fs.OpenCache(dir)
	if err != nil {
		return nil, err
	}
	c := fs.New(creds, cache)
	c.Refresh, _ = cmd.Flags().GetBool(refreshFlag)
	return c, nil
}

// Explain is err with what to do about it, for the errors a user can fix: no
// application set up, no login.
func Explain(cmd *cobra.Command, err error) error {
	if errors.Is(err, fs.ErrNotLoggedIn) {
		return fmt.Errorf("%w; run \"%s strava login\" first", err, cmd.Root().Name())
	}
	return err
}

func loginCmd() *cobra.Command {
	var paste bool
	c := &cobra.Command{
		Use:   "login",
		Short: "Allow access to your Strava activities, once",
		Long: `login opens Strava in your browser to allow your application to read your
activities, and keeps the token it gets beside your credentials, for you alone.
Strava sends the browser back to this program on localhost.

On a machine without a browser -- one reached over ssh -- --no-browser prints
the address to open elsewhere, and asks for the address Strava then sends that
browser to, which it will fail to open.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := Open(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			o := fs.LoginOptions{Open: openBrowser, Out: cmd.ErrOrStderr()}
			if paste {
				o.Paste = cmd.InOrStdin()
			}
			t, err := client.Login(cmd.Context(), o)
			if err != nil {
				return err
			}
			who := strconv.FormatInt(t.AthleteID, 10)
			if t.AthleteName != "" {
				who = t.AthleteName + " (athlete " + who + ")"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Logged in to Strava as %s; the token is kept in %s\n", who, client.TokenPath())
			return nil
		},
	}
	c.Flags().BoolVar(&paste, "no-browser", false, "print the address to open, and read back the one Strava sends the browser to")
	return c
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke access on Strava, and forget the login and what was fetched",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := Open(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if err := client.Logout(cmd.Context()); err != nil {
				if errors.Is(err, fs.ErrNotLoggedIn) {
					fmt.Fprintln(cmd.OutOrStdout(), "Not logged in to Strava.")
					return nil
				}
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out of Strava: access revoked, and the login and what was fetched forgotten.")
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say which application and login are set up, and what is cached",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			client, err := Open(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			defer w.Flush()
			t, err := fs.LoadToken(client.TokenPath())
			if errors.Is(err, fs.ErrNotLoggedIn) {
				fmt.Fprintf(w, "login\tnone; run \"%s strava login\"\n", cmd.Root().Name())
				return nil
			}
			if err != nil {
				return err
			}
			name := t.AthleteName
			if name == "" {
				name = "athlete"
			}
			fmt.Fprintf(w, "login\t%s (athlete %d), application %s\n", name, t.AthleteID, t.ClientID)
			fmt.Fprintf(w, "allowed\t%s\n", t.Scope)
			fmt.Fprintf(w, "token\t%s, %s\n", client.TokenPath(), expiry(t.ExpiresAt))
			n, oldest := client.Cache().Entries(t.AthleteID)
			if n == 0 {
				fmt.Fprintf(w, "cached\tnothing, in %s\n", client.Cache().Dir())
			} else {
				fmt.Fprintf(w, "cached\t%d answers, the oldest from %s and deleted a day after, in %s\n",
					n, oldest.Local().Format("2006-01-02 15:04"), client.Cache().Dir())
			}
			return nil
		},
	}
}

// expiry says when an access token runs out, which is no matter: it is
// refreshed when it does.
func expiry(at time.Time) string {
	d := time.Until(at).Round(time.Minute)
	if d <= 0 {
		return "access token expired, to be refreshed on next use"
	}
	return "access token valid for " + strings.TrimSuffix(d.String(), "0s") + ", then refreshed"
}

func listCmd() *cobra.Command {
	var after, before string
	var week bool
	var days int
	c := &cobra.Command{
		Use:   "list",
		Short: "List your Strava activities over a stretch of days, with their ids",
		Long: `list prints your Strava activities -- the id to give other commands as
strava:ID, when each started, its sport, distance, moving time and title --
over the last --days days, the week so far with --week, or between --after and
--before, dates in your own time zone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, to, err := window(time.Now(), after, before, week, days, cmd.Flags().Changed("days"))
			if err != nil {
				return err
			}
			client, err := Open(cmd, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			got, err := client.Activities(cmd.Context(), from, to)
			if err != nil {
				return Explain(cmd, err)
			}
			if len(got) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "No Strava activities from %s to %s.\n", from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"))
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "id\tstarted\tsport\tkm\tmoving\ttitle")
			for _, s := range got {
				fmt.Fprintf(w, "strava:%d\t%s\t%s\t%.2f\t%s\t%s\n", s.ID, s.Start.Local().Format("2006-01-02 15:04"),
					s.SportType, s.Distance/1000, clock(time.Duration(s.MovingTime)*time.Second), s.Name)
			}
			return w.Flush()
		},
	}
	f := c.Flags()
	f.IntVar(&days, "days", 7, "list the last this many days")
	f.BoolVar(&week, "week", false, "list the week so far, from Monday")
	f.StringVar(&after, "after", "", "list from this date, YYYY-MM-DD")
	f.StringVar(&before, "before", "", "list until this date, YYYY-MM-DD, not including it")
	return c
}

// window is the stretch list covers, in now's time zone.
func window(now time.Time, after, before string, week bool, days int, daysSet bool) (time.Time, time.Time, error) {
	day := func(s, flag string) (time.Time, error) {
		t, err := time.ParseInLocation("2006-01-02", s, now.Location())
		if err != nil {
			return time.Time{}, fmt.Errorf("--%s %q is not a date like 2026-10-25", flag, s)
		}
		return t, nil
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case week && (after != "" || before != "" || daysSet):
		return time.Time{}, time.Time{}, errors.New("--week is a stretch of its own; give it alone")
	case week:
		back := (int(now.Weekday()) + 6) % 7
		return midnight.AddDate(0, 0, -back), now, nil
	case after != "" || before != "":
		if daysSet {
			return time.Time{}, time.Time{}, errors.New("--days and --after/--before are two ways to say the stretch; give one")
		}
		from, to := time.Time{}, now
		var err error
		if after != "" {
			if from, err = day(after, "after"); err != nil {
				return from, to, err
			}
		}
		if before != "" {
			if to, err = day(before, "before"); err != nil {
				return from, to, err
			}
		}
		if !from.IsZero() && !to.After(from) {
			return from, to, errors.New("--before is not after --after")
		}
		return from, to, nil
	}
	if days < 1 {
		return time.Time{}, time.Time{}, fmt.Errorf("--days %d: give one or more", days)
	}
	return midnight.AddDate(0, 0, 1-days), now, nil
}

func clock(d time.Duration) string {
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// openBrowser shows url in the user's browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

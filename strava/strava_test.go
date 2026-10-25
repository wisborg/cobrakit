package strava

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestWindow(t *testing.T) {
	loc := time.FixedZone("test", 10*3600)
	now := time.Date(2026, 10, 25, 18, 30, 0, 0, loc) // a Sunday
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, loc) }
	for _, c := range []struct {
		name          string
		after, before string
		week          bool
		days          int
		daysSet       bool
		from, to      time.Time
		err           string
	}{
		{name: "the last week by default", days: 7, from: day(19), to: now},
		{name: "today", days: 1, daysSet: true, from: day(25), to: now},
		{name: "the week so far, from Monday", week: true, days: 7, from: day(19), to: now},
		{name: "between dates", after: "2026-10-01", before: "2026-10-08", days: 7, from: day(1), to: day(8)},
		{name: "from a date to now", after: "2026-10-20", days: 7, from: day(20), to: now},
		{name: "backwards", after: "2026-10-08", before: "2026-10-01", days: 7, err: "not after"},
		{name: "not a date", after: "last week", days: 7, err: "not a date"},
		{name: "two ways at once", after: "2026-10-01", days: 3, daysSet: true, err: "give one"},
		{name: "week with dates", week: true, after: "2026-10-01", days: 7, err: "alone"},
	} {
		from, to, err := window(now, c.after, c.before, c.week, c.days, c.daysSet)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err %v, want %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || !from.Equal(c.from) || !to.Equal(c.to) {
			t.Errorf("%s: %v to %v (%v), want %v to %v", c.name, from, to, err, c.from, c.to)
		}
	}
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "prog"}
	Add(root)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestStatus_BeforeAnySetUpSaysWhatToDo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("STRAVA_CONFIG", "")
	t.Setenv("STRAVA_CLIENT_ID", "")
	t.Setenv("STRAVA_CLIENT_SECRET", "")
	_, err := run(t, "strava", "status")
	if err == nil || !strings.Contains(err.Error(), "settings/api") || !strings.Contains(err.Error(), "localhost") {
		t.Errorf("err %v; want how to register an application", err)
	}
	t.Setenv("STRAVA_CLIENT_ID", "4242")
	t.Setenv("STRAVA_CLIENT_SECRET", "invented")
	out, err := run(t, "strava", "status")
	if err != nil || !strings.Contains(out, `run "prog strava login"`) {
		t.Errorf("out %q, err %v; want to be told to log in", out, err)
	}
	out, err = run(t, "strava", "logout")
	if err != nil || !strings.Contains(out, "Not logged in") {
		t.Errorf("logout before login: %q, %v", out, err)
	}
}

func TestAdd_GivesEveryCommandTheConfigFlag(t *testing.T) {
	root := &cobra.Command{Use: "prog"}
	other := &cobra.Command{Use: "summary", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(other)
	Add(root)
	root.SetArgs([]string{"summary", "--strava-config", "/somewhere/strava.yaml", "--strava-refresh"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if v, _ := other.Flags().GetString(configFlag); v != "/somewhere/strava.yaml" {
		t.Errorf("--strava-config on another command read as %q", v)
	}
	t.Setenv("STRAVA_CLIENT_ID", "4242")
	t.Setenv("STRAVA_CLIENT_SECRET", "invented")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("STRAVA_CONFIG", "")
	other.Flags().Set(configFlag, "")
	c, err := Open(other, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Refresh {
		t.Error("--strava-refresh did not reach the client")
	}
}

package strava

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/wisborg/fitactivity"
	fs "github.com/wisborg/fitactivity/strava"
)

func TestID(t *testing.T) {
	for _, c := range []struct {
		in string
		id int64
		ok bool
	}{
		{"strava:1234", 1234, true},
		{"https://www.strava.com/activities/1234", 1234, true},
		{"https://www.strava.com/activities/1234/overview", 1234, true},
		{"strava:", 0, false},
		{"strava:abc", 0, false},
		{"run.fit", 0, false},
		{"/tmp/strava:1234.fit", 0, false},
	} {
		if id, ok := ID(c.in); id != c.id || ok != c.ok {
			t.Errorf("ID(%q) = %d, %v; want %d, %v", c.in, id, ok, c.id, c.ok)
		}
	}
	if Stem("strava:1234") != "strava-1234" || Stem("/a/b/run.fit") != "run" {
		t.Errorf("stems %q, %q", Stem("strava:1234"), Stem("/a/b/run.fit"))
	}
}

// cachedActivity is a login and a cached answer for an invented activity,
// in directories of the test's own, so a strava: source is read without
// asking Strava anything.
func cachedActivity(t *testing.T) {
	t.Helper()
	config, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv(fs.EnvConfig, "")
	t.Setenv(fs.EnvClientID, "4242")
	t.Setenv(fs.EnvClientSecret, "invented")
	if err := fs.SaveToken(filepath.Join(config, "strava-token.json"), fs.Token{
		ClientID: "4242", AthleteID: 77, AccessToken: "a", RefreshToken: "r",
		ExpiresAt: time.Now().Add(6 * time.Hour), Scope: "read,activity:read_all",
	}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache, "fitactivity", "strava", "77")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	answer := `{"id":1001,"name":"Invented Run","sport_type":"Run","start_date":"2026-04-02T06:00:00Z",
		"streams":[{"type":"time","data":[0,1,2]},{"type":"latlng","data":[[10,20],[10.0001,20],[10.0002,20]]}]}`
	if err := os.WriteFile(filepath.Join(dir, "1001.streams.json"), []byte(answer), 0o600); err != nil {
		t.Fatal(err)
	}
}

func command() *cobra.Command {
	root := &cobra.Command{Use: "prog"}
	cmd := &cobra.Command{Use: "summary"}
	root.AddCommand(cmd)
	Add(root)
	return cmd
}

func TestLoader_AStravaSource(t *testing.T) {
	cachedActivity(t)
	read := func(string) (*fitactivity.Track, error) {
		t.Fatal("a strava: source read as a file")
		return nil, nil
	}
	tr, err := Loader(command(), read, Fetch{})("strava:1001")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "Invented Run" || len(tr.Samples) != 3 || tr.SourcePath != "strava:1001" {
		t.Errorf("track %q %q, %d samples", tr.SourcePath, tr.Title, len(tr.Samples))
	}
}

func TestLoader_AFileAloneNeverAsksStrava(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(fs.EnvConfig, "")
	t.Setenv(fs.EnvClientID, "")
	t.Setenv(fs.EnvClientSecret, "")
	read := func(string) (*fitactivity.Track, error) { return &fitactivity.Track{}, nil }
	if _, err := Loader(command(), read, Fetch{})("run.fit"); err != nil {
		t.Fatalf("a file without Attach: %v", err)
	}
	_, err := Loader(command(), read, Fetch{Attach: true})("run.fit")
	if err == nil || !strings.Contains(err.Error(), "no Strava API application") {
		t.Errorf("Attach without an application: %v", err)
	}
}

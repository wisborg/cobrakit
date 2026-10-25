package strava

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wisborg/fitactivity"
	fs "github.com/wisborg/fitactivity/strava"
)

// ID is the activity a source names on Strava: strava:ID, or the activity's
// page, https://www.strava.com/activities/ID.
func ID(source string) (int64, bool) {
	rest, ok := strings.CutPrefix(source, "strava:")
	if !ok {
		for _, page := range []string{"https://www.strava.com/activities/", "https://strava.com/activities/"} {
			if rest, ok = strings.CutPrefix(source, page); ok {
				rest, _, _ = strings.Cut(rest, "/")
				break
			}
		}
	}
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// Stem is what a source is called where a file is named after it: a file's
// name without its extension, and strava-ID for an activity on Strava,
// whose source has a colon or a whole address in it.
func Stem(source string) string {
	if id, ok := ID(source); ok {
		return "strava-" + strconv.FormatInt(id, 10)
	}
	base := filepath.Base(source)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Fetch is what a command asks Strava for beyond strava: sources.
type Fetch struct {
	// Attach gives a file Strava's details -- title, description,
	// segments -- from the athlete's activity that started when it did.
	Attach bool
	// Gear asks for the brand, model and description of the gear an
	// activity was done in or on: a request more for each.
	Gear bool
}

// Loader reads a command's activities: a file by read, and strava:ID from
// the athlete's own activities on Strava, with what more f asks for.
//
// Strava is asked only for a strava: source or with f.Attach, and a client
// opened only then, so a user with no Strava application set up never meets
// one.
func Loader(cmd *cobra.Command, read func(string) (*fitactivity.Track, error), f Fetch) func(string) (*fitactivity.Track, error) {
	var client *fs.Client
	open := func() (*fs.Client, error) {
		if client != nil {
			return client, nil
		}
		c, err := Open(cmd, cmd.ErrOrStderr())
		if err != nil {
			return nil, err
		}
		client = c
		return c, nil
	}
	return func(source string) (*fitactivity.Track, error) {
		if id, ok := ID(source); ok {
			c, err := open()
			if err != nil {
				return nil, err
			}
			t, err := c.Track(cmd.Context(), id)
			if err == nil && f.Gear {
				err = c.AddGear(cmd.Context(), t)
			}
			return t, Explain(cmd, err)
		}
		t, err := read(source)
		if err != nil || !f.Attach {
			return t, err
		}
		c, err := open()
		if err != nil {
			return nil, err
		}
		s, err := c.Attach(cmd.Context(), t, filepath.Base(source))
		if err != nil {
			return nil, Explain(cmd, err)
		}
		if s == nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: no Strava activity of yours started when %s did\n", cmd.Root().Name(), source)
		} else if f.Gear {
			if err := c.AddGear(cmd.Context(), t); err != nil {
				return nil, Explain(cmd, err)
			}
		}
		return t, nil
	}
}

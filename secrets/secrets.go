// Package secrets gives a program a "secrets" command: which of the secrets
// it uses -- a Strava application's client secret, a map service's key --
// are set and where, and setting or removing one in the system's keychain,
// the same in fitdash and course.
//
// The work is github.com/wisborg/secrets'; this is only the command. Nothing
// it prints holds a secret's value.
package secrets

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/wisborg/secrets"
)

// Lister is some of a program's secrets, with where each is looked for as
// cmd's flags say: --strava-config names the file the Strava application's
// are in.
type Lister func(cmd *cobra.Command) ([]secrets.Secret, error)

// listers are what each program's secrets command lists, by its root.
var listers = map[*cobra.Command][]Lister{}

// Add gives root a "secrets" command for the secrets list gives, as well as
// those an earlier Add gave it: each part of a program that uses secrets
// adds its own.
func Add(root *cobra.Command, list Lister) {
	listers[root] = append(listers[root], list)
	for _, c := range root.Commands() {
		if c.Name() == "secrets" {
			return
		}
	}
	program := root.Name()
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Say where the secrets " + program + " uses are, and keep them in the system's keychain",
		Long: `secrets lists the secrets ` + program + ` uses -- API keys, your Strava
application's client id and secret, your Strava login -- and where each was
found: the environment, the system's keychain, or a file you wrote. Each is
looked for in that order. Nothing it prints is a secret's value.

"set" keeps one in the keychain -- the macOS Keychain, the Windows Credential
Manager, or a Secret Service such as GNOME Keyring on Linux -- out of the files
a backup or a sync copies:

  ` + program + ` secrets set strava/client_secret

reads it without showing it as you type, or from standard input when that is
not a terminal. $` + secrets.EnvStore + `=file keeps the keychain out of it
altogether.`,
	}
	cmd.AddCommand(statusCmd(), setCmd(), deleteCmd())
	root.AddCommand(cmd)
}

// all is every secret cmd's program uses, by name.
func all(cmd *cobra.Command) ([]secrets.Secret, error) {
	var list []secrets.Secret
	for _, l := range listers[cmd.Root()] {
		got, err := l(cmd)
		if err != nil {
			return nil, err
		}
		list = append(list, got...)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// named is the secret of that name, or an error naming those there are.
func named(cmd *cobra.Command, name string) (secrets.Secret, error) {
	list, err := all(cmd)
	if err != nil {
		return secrets.Secret{}, err
	}
	var names []string
	for _, s := range list {
		if s.Name == name {
			return s, nil
		}
		if !s.Kept {
			names = append(names, s.Name)
		}
	}
	return secrets.Secret{}, fmt.Errorf("%s uses no secret %q; it uses %s", cmd.Root().Name(), name, strings.Join(names, ", "))
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say which secrets are set, and where",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := all(cmd)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "keychain\t%s\n\n", secrets.KeychainStatus())
			fmt.Fprintln(w, "secret\tfound in\twhat it is")
			for _, s := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, found(s), s.About)
			}
			return w.Flush()
		},
	}
}

// found is where s is, or why it is not.
func found(s secrets.Secret) string {
	_, from, err := s.Get()
	switch {
	case err == nil:
		return from.Where(s.Name)
	case errors.Is(err, secrets.ErrNotFound):
		return "not set"
	default:
		return "unreadable: " + err.Error()
	}
}

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set NAME",
		Short: "Keep a secret in the system's keychain",
		Long: `set keeps the secret NAME in the system's keychain: typed without being
shown, when standard input is a terminal, or read from it -- a password
manager's output piped in -- when it is not.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := named(cmd, args[0])
			if err != nil {
				return err
			}
			if s.Kept {
				return fmt.Errorf("%s is kept by %s itself, not set: %s", s.Name, cmd.Root().Name(), s.About)
			}
			if !secrets.KeychainUsed() {
				return fmt.Errorf("no keychain is used: %s; put %s in %s instead", secrets.KeychainStatus(), s.Name, elsewhere(s))
			}
			v, err := read(cmd, s.Name)
			if err != nil {
				return err
			}
			if err := (secrets.Keychain{}).Set(s.Name, v); err != nil {
				return err
			}
			where := secrets.Keychain{}.Where(s.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Kept %s in %s.\n", s.Name, where)
			if _, from, err := s.Get(); err == nil && from.Where(s.Name) != where {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: note: %s is also in %s, which is read first\n", cmd.Root().Name(), s.Name, from.Where(s.Name))
			}
			return nil
		},
	}
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete NAME",
		Short: "Remove a secret from the system's keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := named(cmd, args[0])
			if err != nil {
				return err
			}
			if s.Kept {
				return fmt.Errorf("%s is kept by %s itself: %s, and forgotten by logging out", s.Name, cmd.Root().Name(), s.About)
			}
			if err := (secrets.Keychain{}).Delete(s.Name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is not in the keychain.\n", s.Name)
			if _, from, err := s.Get(); err == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: note: %s is still in %s\n", cmd.Root().Name(), s.Name, from.Where(s.Name))
			}
			return nil
		},
	}
}

// elsewhere is where else than the keychain s is looked for.
func elsewhere(s secrets.Secret) string {
	var where []string
	for _, src := range s.Sources {
		if _, ok := src.(secrets.Keychain); ok {
			continue
		}
		if w := src.Where(s.Name); w != "" {
			where = append(where, w)
		}
	}
	if len(where) == 0 {
		return "the environment"
	}
	return strings.Join(where, " or ")
}

// maxSecret is more than any secret: what is read past it is not one.
const maxSecret = 64 << 10

// read is the secret's value: typed, unseen, at a terminal, or read whole
// from standard input otherwise.
func read(cmd *cobra.Command, name string) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s (not shown): ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", name, err)
		}
		return nonEmpty(name, string(b))
	}
	b, err := io.ReadAll(io.LimitReader(in, maxSecret+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	if len(b) > maxSecret {
		return "", fmt.Errorf("reading %s: more than %d KiB on standard input is not a secret", name, maxSecret>>10)
	}
	return nonEmpty(name, string(b))
}

func nonEmpty(name, v string) (string, error) {
	if v = strings.TrimSpace(v); v == "" {
		return "", fmt.Errorf("%s: nothing given", name)
	}
	return v, nil
}

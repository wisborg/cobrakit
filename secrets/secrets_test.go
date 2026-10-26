package secrets

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wisborg/secrets"
)

// The keychain these tests reach is one in memory, never the user's own.
// Every value is invented.
func TestMain(m *testing.M) {
	secrets.MockKeychain(nil)
	os.Exit(m.Run())
}

func program() *cobra.Command {
	root := &cobra.Command{Use: "prog"}
	env := secrets.Env{"svc/key": "TEST_SVC_KEY"}
	Add(root, func(*cobra.Command) ([]secrets.Secret, error) {
		return []secrets.Secret{{Name: "svc/key", About: "an invented key", Sources: secrets.Chain{env, secrets.Keychain{}}}}, nil
	})
	Add(root, func(*cobra.Command) ([]secrets.Secret, error) {
		return []secrets.Secret{{Name: "svc/token", About: "an invented login", Kept: true, Sources: secrets.Chain{secrets.Keychain{}}}}, nil
	})
	return root
}

func run(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	root := program()
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errs.String(), err
}

// Set keeps what is piped in, status says where it is without saying what
// it is, and delete removes it.
func TestSetStatusDelete(t *testing.T) {
	t.Setenv(secrets.EnvStore, "")
	t.Setenv("TEST_SVC_KEY", "")
	out, _, err := run(t, "", "secrets", "status")
	if err != nil || !strings.Contains(out, "svc/key") || !strings.Contains(out, "not set") || !strings.Contains(out, "svc/token") {
		t.Errorf("status before: %q, %v", out, err)
	}
	if out, _, err := run(t, "s3cret-value\n", "secrets", "set", "svc/key"); err != nil || !strings.Contains(out, "Kept svc/key in") {
		t.Fatalf("set: %q, %v", out, err)
	}
	if v, _ := (secrets.Keychain{}).Get("svc/key"); v != "s3cret-value" {
		t.Errorf("keychain holds %q", v)
	}
	out, _, err = run(t, "", "secrets", "status")
	if err != nil || !strings.Contains(out, "wisborg/svc, key") || strings.Contains(out, "s3cret") {
		t.Errorf("status after: %q, %v", out, err)
	}
	// The environment is read first, and set says so.
	t.Setenv("TEST_SVC_KEY", "from-env")
	if _, errs, _ := run(t, "s3cret-value", "secrets", "set", "svc/key"); !strings.Contains(errs, "$TEST_SVC_KEY, which is read first") {
		t.Errorf("no note of the environment: %q", errs)
	}
	t.Setenv("TEST_SVC_KEY", "")
	if out, _, err := run(t, "", "secrets", "delete", "svc/key"); err != nil || !strings.Contains(out, "not in the keychain") {
		t.Errorf("delete: %q, %v", out, err)
	}
	if _, err := (secrets.Keychain{}).Get("svc/key"); err == nil {
		t.Error("still in the keychain")
	}
}

// A login is the program's to keep, a name it does not use is a mistake,
// and nothing is nothing to keep.
func TestSet_Refused(t *testing.T) {
	t.Setenv(secrets.EnvStore, "")
	if _, _, err := run(t, "x", "secrets", "set", "svc/token"); err == nil || !strings.Contains(err.Error(), "kept by prog itself") {
		t.Errorf("a login: %v", err)
	}
	if _, _, err := run(t, "x", "secrets", "set", "svc/nope"); err == nil || !strings.Contains(err.Error(), "it uses svc/key") {
		t.Errorf("an unknown name: %v", err)
	}
	if _, _, err := run(t, " \n", "secrets", "set", "svc/key"); err == nil || !strings.Contains(err.Error(), "nothing given") {
		t.Errorf("nothing: %v", err)
	}
	t.Setenv(secrets.EnvStore, "file")
	if _, _, err := run(t, "x", "secrets", "set", "svc/key"); err == nil || !strings.Contains(err.Error(), "no keychain is used") || !strings.Contains(err.Error(), "$TEST_SVC_KEY") {
		t.Errorf("no keychain: %v", err)
	}
}

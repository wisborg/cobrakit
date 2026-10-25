# cobrakit

What the [cobra](https://github.com/spf13/cobra) commands of
[fitdash](https://github.com/wisborg/fitdash),
[videofx](https://github.com/wisborg/videofx) and
[course](https://github.com/wisborg/course) share, kept in one place rather
than copied from one to the next.

## completion

cobra gives a program a `completion` command that prints a shell's completion
script. What it does not do is put the script where the shell will read it,
which is the step a person wants and the one most easily got wrong: a script
in a directory the shell never searches does nothing, and says nothing about
it.

`completion.AddInstall` adds `completion install` beneath it. It writes the
script into a directory the shell reads -- for zsh, a writable directory
already on the `fpath` the shell itself reports, or `~/.local/share/zsh/
site-functions`; for bash, bash-completion's user directory -- and then says
whether that directory is one the shell actually reads. Where it is not, it
prints the line to add, and where. It never edits a startup file.

```go
root := &cobra.Command{Use: "fitdash"}
// ... root's subcommands ...
completion.AddInstall(root)
root.Execute()
```

```
fitdash completion install              # for the shell $SHELL names
fitdash completion install --shell all  # zsh and bash
fitdash completion install --dry-run    # say where, write nothing
```

A program that has turned cobra's completion command off
(`CompletionOptions.DisableDefaultCmd`) gets nothing added.

## strava

`strava.Add` adds `strava login`, `logout`, `status` and `list` beneath a program's root, and
`--strava-config` to every command, so fitdash, videofx and course connect to the user's own
Strava activities the same way. `strava.Open` is the client a command then fetches with, from
fitactivity's `strava` package, which does the work and keeps Strava's API Policy.

```
course strava login             # once: allow access in the browser
course strava list --week       # this week's activities, with their ids
course summary strava:1234567890
course strava logout            # revoke access, and forget the login and what was fetched
```

Each user registers their own Strava API application, with `localhost` as its Authorization
Callback Domain, and puts its client id and secret in `~/.config/strava.yaml` under `strava:` as
`client_id` and `client_secret`, or in `$STRAVA_CLIENT_ID` and `$STRAVA_CLIENT_SECRET`.

## Building and testing

```
make gates   # gofmt + vet + test, one status line each
make race    # the tests as CI runs them
```

## Dependencies

cobra (Apache-2.0), and beneath it pflag (BSD-3-Clause) and mousetrap
(Apache-2.0); see [NOTICE](NOTICE).

## Licence

Apache License 2.0 — see [LICENSE](LICENSE).

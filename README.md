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
Strava activities the same way. `strava.Loader` reads a command's activities -- `strava:ID` from Strava, a file as the program reads it, and with `Fetch{Attach: true}` a file given Strava's title, description and segments -- and `strava.Open` is the client underneath it, from
fitactivity's `strava` package, which does the work and keeps Strava's API Policy.

```
course strava login             # once: allow access in the browser
course strava list --week       # this week's activities, with their ids
course summary strava:1234567890
course summary --strava-refresh strava:1234567890   # ask again, for an activity edited since
course strava logout            # revoke access, and forget the login and what was fetched
```

Each user registers their own Strava API application, with `localhost` as its Authorization
Callback Domain, and puts its client id and secret in `~/.config/strava.yaml` under `strava:` as
`client_id` and `client_secret`, in `$STRAVA_CLIENT_ID` and `$STRAVA_CLIENT_SECRET`, or in the
system's keychain with `secrets set`. The login is kept in the keychain where there is one.

## secrets

`secrets.Add` gives a program a `secrets` command for the secrets it uses: which are set, and
where -- the environment, the system's keychain, a file the user wrote -- and setting or removing
one in the keychain. Each part of a program that uses secrets adds its own; `strava.Add` adds the
Strava application's and the login. Nothing it prints is a secret's value. The work is
[secrets](https://github.com/wisborg/secrets)'.

```
course secrets status                        # what is set, and where
course secrets set strava/client_secret      # typed unseen, or piped in
course secrets delete strava/client_secret
```

## weather

`weather.AddFlags` gives a command `--weather`, `--wind-exposure` and `--acclimatised`, and
`weather.Resolve` fetches the weather an activity was done in when asked -- from Open-Meteo, through
fitactivity's `weather` package, which tells Open-Meteo where and when it was, so only with the
flag. `WriteNote` says what the weather was, whether this run sent anything, and the credit its
licence asks for; `CostsOf` and `WriteCosts` what the wind, the heat and the altitude cost a run,
each with what it rests on; `JSONOf` the same for JSON and YAML, with the same names in every
program. fitdash and course both say it through this, in the same words.

## Building and testing

```
make gates   # gofmt + vet + test, one status line each
make race    # the tests as CI runs them
```

## Dependencies

cobra (Apache-2.0), and beneath it pflag (BSD-3-Clause) and mousetrap
(Apache-2.0); x/term (BSD-3-Clause), to read a secret without showing it; and
fitactivity and secrets, with what they are built on; see [NOTICE](NOTICE).

## Licence

Apache License 2.0 — see [LICENSE](LICENSE).

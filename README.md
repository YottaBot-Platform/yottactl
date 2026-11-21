# yottactl

yottactl is YottaBot's command line tool. Sign in once with your browser, then
manage your organization from a terminal or a script: its people, groups,
roles and policies, and who holds them. Every command acts as you, with your
roles, and is in your organization's audit record under your name.

## Install

macOS and Linux:

```bash
curl -fsSL https://yotta.bot/install-yottactl.sh | sh
```

Or with [Homebrew](https://brew.sh):

```bash
brew install yottabot-platform/tap/yottactl
```

Windows, in PowerShell:

```powershell
irm https://yotta.bot/install-yottactl.ps1 | iex
```

Or download a build from the Access Portal's **Downloads**, or from this
repository's [releases](https://github.com/YottaBot-Platform/yottactl/releases).
Each release lists its files' SHA-256 checksums in `SHA256SUMS`. More at
[yotta.bot/platform/install](https://yotta.bot/platform/install).

Or build it from this repository's source, with Go:

```bash
go install github.com/YottaBot-Platform/yottactl/cmd/yottactl@latest
```

## Get started

```bash
yottactl login                 # approve a code in your browser
yottactl org users list
yottactl org groups add-member <group-id> --set person_id=<person-id>
yottactl org boo usage --query group_by=group   # your organization's Boo usage, by group
yottactl org token             # your token, for calling the API with curl
```

Every command has a page in the API reference, with the permission it needs:
[yotta.bot/platform](https://yotta.bot/platform).

## Help

- Documentation: [yotta.bot/platform](https://yotta.bot/platform) and
  [yotta.bot/support](https://yotta.bot/support)
- Bugs and requests: open an issue here. The source here is copied from
  YottaBot's own repository at each release, so we can't merge pull requests;
  an issue gets the change made there.
- Security: see [SECURITY.md](SECURITY.md); please don't open an issue

## License

© 2026 YottaBot, Inc. All rights reserved. See [LICENSE.md](LICENSE.md).

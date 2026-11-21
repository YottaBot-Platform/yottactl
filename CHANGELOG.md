# Changelog

## 1.1.0

- `yottactl org boo`: your organization's Boo usage, balance and receipts,
  each person's allowance, and its plans, extra credits, credit requests and
  cloud machines setting, from the Boo · Admin API
  ([yotta.bot/platform/boo-admin](https://yotta.bot/platform/boo-admin)).
  It reads only; changes are made in Boo · Admin.

## 1.0.2

- yottactl's source is in this repository, and each release is tagged on the
  source it was built from. Build it with
  `go install github.com/YottaBot-Platform/yottactl/cmd/yottactl@latest`.
- A build from source says its version (`yottactl --version`) when Go
  installed it at one.

## 1.0.1

- `yottactl --help` now says what yottactl is for and where to start
  (`yottactl login`), and links to the docs at yotta.bot/platform.
- The help no longer lists commands that have nothing behind them yet. They
  still run, so a script that calls one gets the same answer as before.

## 1.0.0

- First release: `yottactl login`, `yottactl org token`, and a command for
  every endpoint of the Identity API under `yottactl org`.
- Builds for macOS, Linux and Windows, on x64 and ARM64. The macOS builds are
  signed and notarized.

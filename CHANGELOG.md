# Changelog

## 1.2.0

- An organization's service account signs in for itself, with no browser:
  `yottactl login --service-account sa-<id> --key <file>`, or in a pipeline
  `YOTTA_SERVICE_ACCOUNT` with `YOTTA_PRIVATE_KEY` or `YOTTA_PRIVATE_KEY_FILE`.
  In GitHub Actions, `YOTTA_SERVICE_ACCOUNT` alone signs in under the account's
  trust policy, with no key.
- `yottactl org service-accounts keygen` makes the key: the private half to a
  new file, the public half to paste into Identity.
- Signed in as a service account, `yottactl org token --surface identity` (or
  `boo-admin`) prints a 15-minute token for curl. yottactl keeps that token
  until a minute before it ends, so a run of commands signs in once.
- More at [yotta.bot/platform/authentication](https://yotta.bot/platform/authentication).

## 1.1.2

- yottactl is open source, under the Apache License 2.0 (LICENSE, NOTICE).
  Releases before 1.1.2 keep the terms they were published under.
- Every archive carries LICENSE, NOTICE and THIRD_PARTY_NOTICES, the licenses
  of the code yottactl is built with.

## 1.1.1

- `yottactl login` remembers where you signed in: if your company signs in at
  its own address, pass `--issuer https://<company>.yotta.bot` once, and later
  sign-ins go there too (it says so). `--issuer` still chooses another, and
  `yottactl logout` keeps the address.
- `yottactl login --help` says what `--issuer` is for.
- `yottactl org boo usage --query group_by=group` names each group.

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

# Changelog

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

// Package launcher is the supervising launcher behind the mantle command.
//
// The launcher supervises every mantle-ui build, so it imports only the standard library (enforced by TestStdlibOnly) and stays
// small. It:
//
//   - dispatches argv: its own commands (versions, rollback, doctor, --safe)
//     first, then -p/--print and claude subcommands straight to claude via
//     exec, and everything else to the supervised UI;
//   - starts ~/.mantle/current/mantle-ui in its own process group (so Bubble
//     Tea's suspend stops both), forwards SIGTERM and SIGHUP, and waits;
//   - on an abnormal exit restores termios, writes terminal reset sequences,
//     kills the engine process groups listed in the run file, and prints a
//     notice with the log path;
//   - keeps a newly promoted build on probation until it writes a healthy
//     marker, and flips current back to last-good after two failed launches;
//   - relaunches after exit code 75 with the handoff args from the run file.
//
// # Protocol with mantle-ui
//
// Environment set by the launcher (see the Env* constants): MANTLE_HOME,
// MANTLE_BUILD_ID, MANTLE_LAUNCHER_PID, MANTLE_PROBATION=1 while the build is
// on probation, MANTLE_SAFE=1 for `mantle --safe`.
//
// mantle-ui writes $MANTLE_HOME/run/<pid>.json (RunFile, via WriteRunFile)
// at start, and updates it with the session id, engine process groups and
// handoff args as they change. It writes the healthy marker (MarkHealthy)
// once the first frame is drawn, the engine has initialized and it has been
// alive for 20 s. It removes the run file on a clean exit.
//
// Exit codes: 0 normal, 75 restart requested, 64 usage error (not a crash),
// anything else is a crash.
//
// Primary owner: plan 10 (docs/plans/10-selfmod-launcher.md).
package launcher

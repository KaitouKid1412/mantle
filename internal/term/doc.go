// Package term groups mantle's terminal services. They have no Bubble Tea dependency:
// each returns plain values or byte sequences, and features/chrome wraps them in Cmds.
//
//   - terminal: emulator and multiplexer detection, tmux passthrough.
//   - statusline: the statusLine / subagentStatusLine runner and payload.
//   - notify: desktop notifications and the bell, and when to send them.
//   - osc: OSC 9;4 progress, OSC 8 hyperlinks, window titles.
//   - clipboard: copy via platform tools or OSC 52.
//   - prbadge: PR/MR status for the footer, issue and footer links.
//   - focus: terminal focus and user activity.
//
// Primary owner: plan 07 (docs/plans/07-chrome-terminal.md).
package term

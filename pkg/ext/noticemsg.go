package ext

// Addition proposed by plan 02 (engine restart fallback); additive.

// NoticeMsg shows a notice from code that has no Ctx: a Cmd, a goroutine, or the
// engine layer (for example "Started a new session: …" after a failed resume). The
// host handles it like Ctx.Notify.
type NoticeMsg struct {
	Notice Notice
}

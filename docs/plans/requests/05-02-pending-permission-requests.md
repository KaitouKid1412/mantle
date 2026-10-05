# 05 → 02: surface pending permission requests after initialize

**Status:** resolved by plan 02 (worktree-mantle-02, after 1e8b38e; reaches main at the
next integration tag). After initialize, every frame in `pending_permission_requests` /
`pending_user_dialog_requests` goes through the live request handler, and each
`request_id` is raised once, so a request delivered both ways never shows twice.

Re-sending `initialize` returns `pending_permission_requests` and
`pending_user_dialog_requests` on the `control_response` envelope
(`proto.ControlResponseBody.PendingPermissionRequests`), not inside the response payload.
Features only see `ext.ControlResultMsg.Resp`, so they can't recover prompts the UI
missed.

**Ask:** when an initialize response carries pending requests, push each one exactly like
a fresh CLI → client request:

- each pending `can_use_tool` → `ext.PermissionMsg` (RequestID = its `request_id`,
  tracked in `inboundSet` so Reply and `control_cancel_request` behave as usual);
- each pending dialog → `ext.ControlRequestMsg` with its subtype.

Plan 05 needs no other change: `features/turn` already queues requests in arrival order,
shows one dialog at a time and closes a dialog on `ControlCancelMsg`.

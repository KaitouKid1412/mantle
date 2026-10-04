# 05 → 02: surface pending permission requests after initialize

**Status:** open (M2, PARITY PD-18).

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

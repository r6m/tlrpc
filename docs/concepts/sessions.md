# Sessions

Sessions are keyed by `AuthKeyID` and attached to request context.

Current session state (`session.Session`):

- `SeqNo`: outgoing sequence progression.
- `RecentMsgIDs`: dedupe window cache.
- `UserID`: authenticated user binding (if set).
- `Layer`: negotiated/app layer.
- `Data`: per-session user storage (`sync.Map`).

Runtime ownership:

- `conn.go` fetches/creates session from `session.Manager` by auth key.
- Session is touched/saved during request handling.
- Context helpers expose session data:
  - `SessionFromContext`
  - `LayerFromContext`
  - `AuthKeyIDFromContext`
  - `UserIDFromContext`

What is not fully modeled yet:

- Full cross-node session replication semantics.
- Complete resend/state recovery semantics.

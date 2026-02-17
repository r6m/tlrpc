# Wrappers

Wrapper RPC methods are runtime/internal control wrappers around a real query.

Common wrappers:

- `invokeWithLayer(layer:int, query:!X) = X`
- `initConnection(..., query:!X) = X`
- `invokeAfterMsg(msg_id:long, query:!X) = X`
- `invokeAfterMsgs(msg_ids:Vector<long>, query:!X) = X`
- `invokeWithoutUpdates(query:!X) = X`

Behavior model:

- Wrapper is decoded first.
- Runtime applies wrapper side effects (layer/client metadata/dependency semantics).
- Runtime unwraps `query:!X`.
- Inner request is dispatched normally.
- Returned value is the inner `X` response.

These wrapper methods are not intended as user service handlers.

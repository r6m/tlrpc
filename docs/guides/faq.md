# FAQ

## Why does dispatch use constructor IDs?

MTProto/TL wire payloads identify request types by constructor ID, so runtime routing uses that ID directly.

## Should I use `WithInterceptor` or `WithUnaryInterceptor`?

Use `WithUnaryInterceptor` for new code. `WithInterceptor` exists as a compatibility adapter.

## Do generated requests expose method names?

Yes. Generated request structs implement `Method() string`.

## Are pushed updates implemented?

Core request/response flow is implemented. A full pushed-updates subsystem is not fully implemented yet.

## How do I understand end-to-end request flow?

Read [Dispatch Pipeline](../concepts/dispatch-pipeline.md).

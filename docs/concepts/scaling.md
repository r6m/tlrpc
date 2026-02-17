# Scaling

Go-focused scaling strategies for TLRPC deployments:

- Multi-instance stateless frontends + sticky routing by `AuthKeyID`.
- Shared auth/session backing stores when stickiness is not guaranteed.
- Optional fanout bus (NATS/Kafka/Redis streams) for cross-instance update delivery.
- In-process concurrency limits around expensive handlers or downstream dependencies.
- Backpressure via connection/read/write limits and queue sizing.

Practical baseline:

1. Start with sticky routing + local hot session cache.
2. Add shared session store for failover.
3. Add fanout bus only when pushed updates require cross-instance delivery.

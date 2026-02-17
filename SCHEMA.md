## TL Schema Structure and How TLRPC Uses It

TLRPC treats Telegram TL schemas as two different things: **data types** and **RPC methods**. They share the same textual format, so you must use the schema section markers (or equivalent metadata) to classify them correctly.

### TL Files Have Two Sections

A standard TL schema file is divided into:

* `---types---`
  Defines **constructors** (data objects) and **abstract/base types** (tagged unions).

* `---functions---`
  Defines **methods** (RPC request objects) and their return types.

Both types and methods use the same syntax:

```
name#<id> <fields...> = <ReturnType>;
```

So **the section matters**. Syntax alone is not enough.

### Types

Under `---types---`, each line defines a **constructor** that produces a type:

```tl
inputPeerUser#dde8a54c user_id:long access_hash:long = InputPeer;
```

* `InputPeer` is an **abstract/base type** (a tagged union).
* `inputPeerUser` is a **constructor** (variant) of `InputPeer`.
* `#dde8a54c` is the **constructor ID** used on the wire to identify the variant.
* Fields are serialized in the listed order.

TLRPC codegen maps base types to Go interfaces and constructors to concrete structs.

### Methods

Under `---functions---`, each line defines an **RPC method**:

```tl
contacts.getSaved#82f1e39f = Vector<SavedContact>;
```

* `contacts.getSaved` is a **method** (RPC request object).
* `#82f1e39f` is the **method ID** used on the wire.
* Methods may have fields (arguments). If none are listed, it takes no arguments.
* The right side (`= Vector<SavedContact>`) is the **return type**.

TLRPC codegen generates:

* request structs for each method
* service interfaces for user implementations
* `Register*Server()` functions that register method handlers internally

### Built-in TL Primitives

Primitive TL types such as `int`, `long`, `string`, `bytes`, `Bool`, and `Vector<T>` are **built in**. They are not “user-defined types” and must be handled by the codec directly.

### MTProto Transport Objects vs API Objects

Telegram uses MTProto transport envelopes that wrap API payloads (e.g., containers, compression, handshake, result wrappers). These are not user RPC methods and must be handled by the TLRPC runtime.

Examples include:

* `msg_container`
* `message`
* `rpc_result`
* `gzip_packed`

TLRPC handles these internally as part of the protocol stack, before RPC dispatch.

### Wrapper Methods

Telegram defines several **wrapper methods** whose purpose is to wrap and forward an inner request, usually with a field like `query:!X`.

Common examples:

* `invokeWithLayer(layer, query:!X) = X`
* `initConnection(..., query:!X) = X`
* `invokeAfterMsg(msg_id, query:!X) = X`

These are API-layer methods (they appear under `---functions---`), but they are **not exposed to user services**.

TLRPC handles wrapper methods internally by:

1. decoding the wrapper
2. applying its side effects to context/session (e.g., layer, client metadata)
3. unwrapping `query`
4. dispatching the inner method normally
5. returning the inner response as the wrapper result (`X`)

### The `X` Generic Type

Some wrapper methods use the generic type `X`, meaning “returns whatever the inner query returns”:

```tl
invokeWithLayer#... layer:int query:!X = X;
```

In TLRPC:

* `!X` is treated as “any TL object” at runtime (dynamic dispatch by constructor ID).
* The wrapper forwards the inner response unchanged.

### Dispatch Rules

TLRPC’s runtime dispatch flow is:

1. **Transport handling** (MTProto):

   * decrypt, unpack containers, inflate gzip, unwrap results
2. **Wrapper handling** (internal):

   * unwrap `invokeWithLayer`, `initConnection`, `invokeAfterMsg`, etc.
3. **Method routing** (user services):

   * route by method constructor ID to the registered generated handler
4. **Response encoding**:

   * serialize, wrap in MTProto response envelope, encrypt, send

User code only implements “real” API methods, not MTProto envelopes or wrapper forwarding logic.

### Implementation Note

Service registration is gRPC-style:

* services must be registered at startup
* invalid registrations panic (duplicate methods, wrong impl types)
* registration automatically installs method-ID-based handlers into the server’s internal registry

This keeps startup failures explicit and avoids partial server configuration.
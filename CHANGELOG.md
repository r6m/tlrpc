# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-02-18

### Added
- MTProto v2 handshake coverage (`req_pq_multi` -> `req_DH_params` -> `set_client_DH_params` -> `dh_gen_ok`).
- Transport compatibility matrix: TCP abridged/intermediate/padded/full and WebSocket obfuscated2 + padded intermediate.
- Wrapper/envelope pipeline support: `invokeWithLayer`, `initConnection`, `invokeAfter*`, `invokeWithoutUpdates`, plus `msg_container`, `gzip_packed`, `rpc_result`, `msgs_ack`.
- Compatibility harness tooling: `cmd/compat-server`, `cmd/tlrpc-client`, and `compat/client`.
- Guardrails: `check_type_domains.sh`, `check_no_legacy.sh`, `check_no_semantics_creep.sh`.

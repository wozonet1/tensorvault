# ADR-014: Cloud-Native RPC Architecture (gRPC & Buf)

- **Status**: Accepted
- **Date**: 2025-12-03

## Context and Problem Statement

In Phase 1, TensorVault operated as a local CLI tool, interacting directly with the filesystem and database. As we move to Phase 2 (Service Layer) and Phase 3 (HelixPipe Integration), we face new requirements:

1.  **Cross-Language Interoperability**: We need to expose core Go capabilities to Python clients (HelixPipe) efficiently.
2.  **Large File Transport**: Uploading 10GB+ AI models via standard REST/JSON (HTTP/1.1) is inefficient due to Base64 encoding overhead (~33% bloat) and lack of true bidirectional streaming control.
3.  **API Governance**: Managing `.proto` files manually (copy-pasting `validate.proto`, managing `protoc` versions) leads to "Dependency Hell" and inconsistent contracts across teams.

We need a high-performance, strictly typed, and easily maintainable Remote Procedure Call (RPC) architecture.

## Decision Drivers

- **Throughput**: Maximizing network saturation for large binary transfers.
- **Type Safety**: Extending strong typing from Go internals to the API boundary.
- **Maintainability**: Automated code generation and breaking change detection.
- **Developer Experience**: Avoiding the complexity of legacy `protoc` workflows.

## Considered Options

1.  **REST + JSON (HTTP/1.1)**:

    - _Pros_: Ubiquitous; easy to debug with `curl`.
    - _Cons_: No native streaming (Chunked Transfer Encoding is limited); JSON serialization is slow and verbose for binary data; weak contract enforcement.

2.  **Twirp**:

    - _Pros_: Simple RPC framework running on standard HTTP/1.1; minimalist.
    - _Cons_: **Lack of Streaming support.** This is a dealbreaker for our `Ingester`/`Exporter` pipeline which relies on stream processing to maintain low memory footprint.

3.  **gRPC + Legacy `protoc`**:

    - _Pros_: Industry standard performance.
    - _Cons_: Tooling is fragmented; managing third-party dependencies (like `googleapis` or `validate`) is painful.

4.  **gRPC + Buf Toolchain [Selected]**:
    - _Pros_: All benefits of gRPC (HTTP/2 Streaming, Protobuf efficiency) combined with modern tooling (Dependency management, Linting, Breaking Change Detection).

## Decision Outcome

We decided to adopt **gRPC** as the communication protocol and **Buf (v2)** as the build toolchain.

### 1. Protocol Design

- **DataService (Streaming)**: Uses `stream` keywords for file transfer.
  - _Pattern_: **Client-Side Streaming** for Uploads (to support `oneof` metadata/chunk flow); **Server-Side Streaming** for Downloads.
  - _Rationale_: Matches the `io.Reader`/`io.Writer` streaming nature of the underlying Go core.
- **MetaService (Unary)**: Uses standard Request/Response for metadata operations (Commit, GetHead).

### 2. Tooling & Workflow

- **Workspace Mode**: We adopt Buf v2 Workspace structure (`buf.yaml` at root), treating the schema as a first-class citizen.
- **Validation**: We integrate `protovalidate` (via Buf dependencies) to enforce constraints (e.g., Hash length) at the API gateway level, mirroring our internal `types.Hash` logic.

### 3. Payload Strategy

- **`oneof` Pattern**: For Uploads, the request payload is defined as `oneof { FileMeta meta; bytes chunk_data; }`. This allows sending metadata in the first frame and raw binary chunks in subsequent frames, avoiding overhead.

## Consequences

### Positive

- **Performance**: Binary transport over HTTP/2 minimizes overhead for AI model weights.
- **Reliability**: `buf breaking` checks in CI/CD will prevent us from accidentally breaking the Python SDK contract.
- **Clean Code**: The Go server implementation focuses on logic, as validation and routing are handled by the generated code and interceptors.

### Negative

- **Debuggability**: Binary traffic is not human-readable. We must rely on tools like `grpcurl` with reflection enabled, rather than simple `curl`.
- **Browser Incompatibility**: gRPC requires `gRPC-Web` proxy to work with browsers (though not an immediate concern for our Python-centric use case).

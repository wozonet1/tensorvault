# ADR-015: Thin Client Architecture and Two-Phase Upload Protocol

- **Status**: Accepted
- **Date**: 2025-12-06

## Context and Problem Statement

In a Content-Addressable Storage (CAS) system like Git, the client typically performs chunking (CDC) and hashing locally, sending only missing chunks to the server. This minimizes bandwidth but requires heavy computation on the client side.

For TensorVault, our clients include:

1.  **Go CLI**: High performance, capable of local CDC.
2.  **Python SDK (HelixPipe)**: Constrained by GIL, slow at CPU-intensive tasks like rolling hashes (FastCDC).

If we enforce "Client-Side CDC" (Thick Client), the Python SDK will become a performance bottleneck. Furthermore, maintaining identical chunking algorithms across multiple languages (Go, Python, potentially JS/Rust) introduces a high risk of logic drift and data inconsistency.

We need a unified upload protocol that balances **Client Performance**, **Bandwidth Usage**, and **Implementation Complexity**.

## Decision Drivers

- **Client Performance**: The Python SDK must not block training pipelines due to heavy CPU usage.
- **Consistency**: The chunking logic (FastCDC parameters) must be the Single Source of Truth (SSOT).
- **Bandwidth**: Avoid re-uploading large files if they already exist on the server.
- **Simplicity**: Keep client logic dumb and robust.

## Decision Outcome

We decided to adopt a **Thin Client** architecture with a **Two-Phase Upload** protocol.

### 1. Thin Client (Server-Side Chunking)

- **Client Responsibility**:
  - Calculate the full-file **Linear SHA-256** checksum (available in standard libraries of all languages).
  - Stream raw bytes to the server.
- **Server Responsibility**:
  - Receive the stream.
  - Perform **FastCDC** chunking.
  - Calculate Merkle Root.
  - Perform deduplication against S3/Redis.

### 2. Two-Phase Upload Protocol

To mitigate bandwidth waste (uploading a file that already exists), we implement an "Optimistic Deduplication" flow:

- **Phase 1: Pre-check (`CheckFile`)**
  - Client calculates Linear SHA-256 of the file.
  - Client sends `CheckFileRequest(sha256, size)`.
  - Server checks `file_indices` (SQL).
  - If found -> **Instant Success (Seconds)**.
- **Phase 2: Streaming Upload (`Upload`)**
  - If not found -> Client streams raw data via gRPC.
  - Server processes stream, stores chunks, and saves the new Linear Hash -> Merkle Root mapping index.

## Consequences

### Positive

- **High Performance**: Python clients remain fast as they offload CPU-intensive CDC to the Go server.
- **Logic Consistency**: FastCDC logic exists _only_ in the Go server. We can upgrade chunking algorithms without breaking old clients.
- **Simplicity**: Client implementation is trivial (Read File -> Calc SHA256 -> Send).

### Negative

- **Bandwidth Usage**: For files that are _modified_ (not identical), the client must upload the _entire_ file, even if only 1% changed. The server will dedup the chunks, saving storage, but the network transfer still happens.
  - _Mitigation_: In high-bandwidth intranet environments (AI Clusters), this trade-off is acceptable.

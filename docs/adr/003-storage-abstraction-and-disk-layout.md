# ADR-003: Storage Abstraction and Local Disk Implementation

- **Status**: Accepted
- **Date**: 2025-11-25

## Context and Problem Statement

TensorVault objects (Chunks, FileNodes, Trees, Commits) are immutable and content-addressable. While the MVP targets local execution, a production AI infrastructure requires storing these assets on distributed object storage (e.g., AWS S3, MinIO) for scalability and sharing.

Hardcoding local filesystem operations (`os.WriteFile`, `os.ReadFile`) into the core logic would couple the application to the local disk, making future migration to the cloud difficult. Furthermore, storing potentially millions of chunk files in a single local directory can lead to filesystem performance degradation (directory enumeration slowness) or inode exhaustion.

We need a storage architecture that is:

1.  **Backend Agnostic**: The core logic shouldn't care if data lives on NVMe or S3.
2.  **Scalable (Local)**: The local implementation must handle millions of small files efficiently.
3.  **Safe**: Writes must be atomic to prevent corrupted (partial) objects in case of a crash.

## Decision Drivers

- **Portability**: Need to support S3/MinIO in Phase 2.
- **Idempotency**: Writing the same content-addressed object twice should be safe and cheap.
- **Filesystem Performance**: Avoiding "million-file directories."
- **Crash Consistency**: Ensuring objects are either fully written or not written at all.

## Considered Options

1.  **Direct Filesystem Access (Hardcoded)**:
    - _Pros_: Easiest to implement.
    - _Cons_: Tightly coupled; performance issues with flat directories.
2.  **Embedded KV Database (BoltDB/Badger)**:
    - _Pros_: Single file deployment, handles many small keys well.
    - _Cons_: Opaque binary format (hard to debug/observe); concurrency limitations (BoltDB is single-writer).
3.  **Interface Abstraction + Sharded Disk Layout (Selected)**:
    - _Pros_: Decouples logic; sharding solves directory scaling; flat files remain debuggable (`ls`, `cat`).

## Decision Outcome

We decided to define a generic `storage.Store` interface and implement a **Sharded Disk Adapter** for the MVP.

### 1. The Interface

We define a minimal CAS interface in `pkg/storage`:

```go
type Store interface {
    Put(ctx, obj) error
    Get(ctx, hash) (io.ReadCloser, error)
    Has(ctx, hash) (bool, error)
}
```

This abstraction allows us to swap the backend implementation (e.g., `S3Store`, `MemoryStore`) without changing a single line of the `ingester` or `exporter` logic.

### 2. Physical Layout (Sharding)

To mitigate filesystem performance issues with large directory counts, we adopt a **Level-1 Sharding** strategy based on the object's SHA-256 hash.

- **Logic**: Use the first 2 characters of the hex hash as the subdirectory.
- **Example**: Hash `a8fd32...` -> Path `.tv/objects/a8/fd32...`
- **Benefit**: This spreads objects across 256 subdirectories ($16^2$), reducing the number of files per directory by a factor of 256.

### 3. Atomic Write Strategy

To guarantee crash consistency, we prohibit direct overwrites or partial writes:

1.  **Check**: If the object already exists (via `Has`), return success immediately (Idempotency).
2.  **Write Temp**: Write data to a temporary file in the same directory (e.g., `temp-12345`).
3.  **Rename**: Use `os.Rename` to move the temp file to the final path. This operation is atomic on POSIX systems.
    - _Result_: An object file is guaranteed to be complete. We never expose a half-written chunk to readers.

## Consequences

### Positive

- **Cloud Ready**: We can implement `S3Adapter` later by simply satisfying the `Store` interface.
- **Scalability**: The system can handle significantly more files than a flat directory structure.
- **Reliability**: The "Write-Temp-Rename" pattern prevents data corruption during power loss or crashes.

### Negative

- **Inode Usage**: Each chunk consumes one filesystem inode. For massive datasets with tiny chunks (e.g., 4KB), we might hit the disk's inode limit before the capacity limit. (Mitigation: Increase `AvgSize` in FastCDC or use a Packfile strategy in Phase 3).
- **Management Overhead**: Users cannot simply "copy" the repo as easily as a single file; they must move the entire directory structure.

---

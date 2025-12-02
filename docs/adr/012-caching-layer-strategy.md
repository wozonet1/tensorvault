# ADR-012: Caching Layer Strategy and Decorator Pattern

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

In the TensorVault ingestion workflow, **Deduplication** is the critical performance optimization. For AI workflows (e.g., fine-tuning), a new model version may share 99% of its chunks with previous versions.

Currently, the `Has(hash)` check requires a `HeadObject` call to the underlying storage (S3). While faster than uploading, this still incurs significant network latency (RTT ~30-100ms) and costs (S3 API fees). For a 10GB file with ~1.3 million chunks, performing 1.3 million remote checks is a major bottleneck, limiting the ingestion throughput regardless of parallelization.

We need a mechanism to perform "Existence Checks" with sub-millisecond latency, while maintaining the stateless nature of the application workers.

## Decision Drivers

- **Latency**: Existence checks must be instantaneous (< 1ms) to keep up with the CPU hashing speed.
- **Scalability**: The solution must support multiple concurrent CLI/Server instances sharing the same state.
- **Extensibility**: Adding caching logic should not complicate the core storage adapters (S3/Disk).
- **Resilience**: The system must continue to function (albeit slower) if the cache service is unavailable.

## Considered Options

1.  **Local Embedded Cache (e.g., BoltDB/Badger)**:
    - _Pros_: Zero external dependency; fastest access.
    - _Cons_: State is local to the machine. Does not support shared deduplication across a team or cluster.
2.  **In-Memory LRU Cache**:
    - _Pros_: Simplest implementation.
    - _Cons_: Reset on restart; limited by RAM; not shared.
3.  **Distributed Cache (Redis) [Selected]**:
    - _Pros_: Shared state across all workers/users; extremely high throughput; persistence options.
    - _Cons_: Adds an external infrastructure dependency.

## Decision Outcome

We decided to implement a **Distributed Caching Layer** using **Redis**, applied via the **Decorator Pattern**.

### 1. The Decorator Pattern (`CachedStore`)

Instead of modifying `S3Adapter` to talk to Redis, we created a wrapper struct `CachedStore` that implements the `storage.Store` interface.

- **Composition**: It holds a reference to an underlying `backend` Store.
- **Interception**: It intercepts `Has()` and `Put()` calls to inject caching logic.
- **Transparency**: The `Ingester` and other consumers are unaware of the cache's existence; they just see the `Store` interface.

### 2. Cache Strategy

- **Key Namespace**: Keys are prefixed with `tv:obj:<hash>` to avoid collisions and allow easier management (e.g., bulk deletion).
- **Values**: We store a simple flag (`"1"`) indicating existence. We do **not** cache the blob payload itself, as chunks are too large for efficient RAM usage.
- **Flow**:
  - `Has(hash)`: Check Redis.
    - **Hit**: Return `true` immediately (saving S3 RTT).
    - **Miss**: Check S3. If found, **asynchronously** backfill Redis to unblock the main thread.
  - `Put(obj)`: Check Redis (dedup). If missing, upload to S3. Upon success, write to Redis (Write-Through).

### 3. Failure Fallback (Degradation)

The system is designed to be **Resilient to Cache Failures**.

- If Redis is unreachable or times out, the `CachedStore` logs a warning and automatically **falls back** to the underlying backend (S3).
- This ensures that an infrastructure issue in the caching layer causes performance degradation, not service outage.

## Consequences

### Positive

- **Performance**: Deduplication checks for existing chunks are accelerated by orders of magnitude (network RTT vs. memory lookup).
- **Cost Efficiency**: Drastic reduction in S3 `HeadObject` API calls.
- **Clean Architecture**: The core storage logic remains pure; caching complexity is encapsulated in the decorator.

### Negative

- **Infrastructure Overhead**: Requires deploying and maintaining a Redis instance.
- **Consistency Edge Cases**: While rare in CAS (Content Addressable Storage), there is a theoretical possibility of "Cache Pollution" if an object is deleted from S3 manually but remains in Redis. (Mitigated by TTLs and immutable nature of CAS).

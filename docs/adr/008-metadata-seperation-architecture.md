# ADR-008: Metadata Separation Architecture (Dual-Store Model)

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

In the initial design (inspired by Git), all data—including heavy binary chunks, structural Merkle trees, and mutable references (like `HEAD`)—was intended to be stored in the Object Store (Local Disk or S3).

However, as we evolved towards a cloud-native architecture, two critical limitations of using Object Storage for metadata became apparent:

1.  **Query Inefficiency**: To implement features like "Find commits by Author" or "List history since timestamp X," we would need to download and deserialize thousands of Commit objects from S3. Object storage is designed for throughput, not random access search.
2.  **Concurrency Control**: S3 provides strong consistency for _new_ objects but lacks atomic "Compare-And-Swap" (CAS) primitives for updating existing files (like overwriting the `HEAD` reference). Implementing safe concurrent pushes on raw S3 is complex and error-prone.

We need a storage strategy that leverages the cheap scalability of S3 for bulk data while ensuring transactional safety and query speed for metadata.

## Decision Drivers

- **Queryability**: We need SQL-like capabilities to search commit history and lineage.
- **Consistency**: Updating branch references (Refs) requires ACID transactions or atomic CAS support.
- **Scalability**: Heavy payloads (Tensor chunks) must remain on cheap object storage.
- **Separation of Concerns**: Decoupling "Payload" (Immutable) from "State" (Mutable).

## Considered Options

1.  **Pure Object Storage (Git-like)**:

    - _Description_: Store refs as small JSON files on S3.
    - _Pros_: Zero external dependencies; simplest deployment.
    - _Cons_: Handling concurrent `git push` operations requires complex locking mechanisms (e.g., DynamoDB Lock); metadata queries are slow ($O(N)$).

2.  **Embedded KV Store (BoltDB/Badger)**:

    - _Description_: Use a local embedded DB for metadata.
    - _Pros_: Fast; single binary.
    - _Cons_: Does not scale to multiple replicas (Stateful); makes the CLI stateful and harder to deploy on K8s.

3.  **Dual-Store Architecture (S3 + SQL) [Selected]**:
    - _Description_: Use S3 for immutable content (CAS objects) and a Relational Database (PostgreSQL) for mutable refs and search indexes.
    - _Pros_: Best of both worlds. S3 handles PB-level data; Postgres handles transactions and complex queries; stateless application tier.
    - _Cons_: Adds an external dependency (PostgreSQL).

## Decision Outcome

We decided to adopt the **Dual-Store Architecture**.

### 1. Storage Layout

- **Immutable Data (`pkg/storage`)**: Chunks, FileNodes, Trees, and Commit Objects (serialized as CBOR) are stored in the **Object Store** (S3/MinIO). They are addressed by Content-ID (Hash).
- **Mutable Metadata (`pkg/meta`)**: References (`HEAD`, branches) and a _projection_ of Commit metadata (Author, Time, Message, Parent Links) are stored in **PostgreSQL**.

### 2. The "Dual-Write" Commit Flow

When a user executes `tv commit`:

1.  **Put Object**: The full Commit object (CBOR) is uploaded to S3. This is the **Source of Truth** for data integrity.
2.  **Index Metadata**: The commit metadata is inserted into the SQL `commits` table. This serves as the **Search Index**.
3.  **Update Ref**: The `HEAD` reference in the SQL `refs` table is updated atomically.

### 3. Technology Stack

- **ORM**: We use **GORM** to manage SQL interactions, allowing support for PostgreSQL in production and SQLite for local testing.

## Consequences

### Positive

- **Transactional Safety**: We can use SQL transactions and atomic updates to guarantee ref consistency.
- **Rich Queries**: Implementing filtering (e.g., by time range, author, or tags) becomes a simple SQL `WHERE` clause.
- **Performance**: Reading `HEAD` or listing log history is instantaneous (ms-level) compared to S3 list/get operations.

### Negative

- **Complexity**: The system now requires a running database instance.
- **Consistency Risk**: There is a theoretical edge case where writing to S3 succeeds but writing to SQL fails (or vice versa). Currently, we handle this by treating S3 as the primary storage and SQL as a reconstructible index, but cleanup mechanisms may be needed in Phase 4.

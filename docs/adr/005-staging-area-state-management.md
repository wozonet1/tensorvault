### `docs/adr/0005-staging-area-state-management.md`

# ADR-005: Staging Area (Index) Persistence and Format

- **Status**: Accepted
- **Date**: 2025-11-25

## Context and Problem Statement

In a Version Control System (VCS), there is a necessary intermediate state between the Working Directory (local files) and the Repository (committed snapshots). This is commonly known as the "Staging Area" or "Index."

We need a persistent mechanism to track:

1.  Which files have been added/ingested (paths).
2.  Their calculated Merkle Root hashes (CIDs).
3.  Metadata (file size, modification time) to efficiently detect future changes.

Git uses a highly optimized, memory-mapped binary index format. While extremely performant for source code repositories with hundreds of thousands of files, it is opaque and requires specialized tooling (e.g., `git ls-files --debug`) to inspect. For the TensorVault MVP, we prioritize development speed and system transparency over raw metadata performance.

## Decision Drivers

- **Observability**: I want to be able to inspect the index state using standard text tools (`cat`, `vim`) to debug logic errors easily.
- **Simplicity**: The serialization implementation should rely on the standard library without complex parsing logic.
- **MVP Scope**: The initial target usage involves managing AI models (fewer, larger files) rather than massive source trees (many small files like `node_modules`), reducing the immediate need for high-performance indexing.

## Considered Options

1.  **Embedded KV Database (BoltDB / Badger)**:
    - _Description_: Using a persistent B+ Tree or LSM tree.
    - _Pros_: High performance; handles millions of keys; safe concurrent access; robust crash recovery.
    - _Cons_: Binary format is opaque (cannot be read without writing a viewer tool); adds CGO or heavy dependencies; overkill for simple lists.
2.  **Binary Custom Format (Git-like)**:
    - _Description_: A custom compact binary protocol.
    - _Pros_: Extremely compact on disk; fast to load.
    - _Cons_: High complexity to implement parser/serializer; difficult to debug corruption issues.
3.  **Plain JSON (Selected)**:
    - _Description_: Serializing the in-memory map directly to a JSON file.
    - _Pros_: Human-readable; standard library support (`encoding/json`); easiest to debug.
    - _Cons_: Performance penalty (parsing overhead) on very large datasets; non-append-only (requires full file rewrite on every save).

## Decision Outcome

I decided to implement the Index using **Plain JSON**, serialized to `.tv/index`.

### 1. Data Structure

The Index is loaded fully into memory as a Go `map[string]Entry` structure. This provides O(1) lookups for path checking during `add` operations.

```json
{
  "entries": {
    "data/model.bin": {
      "hash": "bafy...",
      "size": 1024,
      "modified_at": "2025-11-25T10:00:00Z"
    }
  }
}
```

### 2. Concurrency Control

To support potential future features like parallel ingestion (e.g., `tv add .`), the in-memory `Index` struct is protected by a `sync.RWMutex` to ensure thread safety during read/write operations.

### 3. Persistence Strategy

I use a **Full Rewrite** strategy for persistence. On `Save()`, the entire map is marshaled to JSON (with indentation for readability) and written to disk atomically.

## Consequences

### Positive

- **High Observability**: I can simply run `cat .tv/index` to verify exactly what will be committed next, significantly speeding up the development and debugging cycle.
- **Simplicity**: The persistence layer is implemented in fewer than 100 lines of code.
- **Portability**: The index file is platform-independent and requires no external database drivers.

### Negative

- **Scalability Limit**: As the number of tracked files grows (e.g., >100k), loading and saving the JSON file will become a CPU/IO bottleneck.
- **Technical Debt**: This decision is explicitly acknowledged as technical debt. When the project evolves to handle massive datasets with millions of files, this layer must be migrated to a more efficient backend (e.g., SQLite or BoltDB).

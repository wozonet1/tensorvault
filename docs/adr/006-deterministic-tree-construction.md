# ADR-006: Deterministic Merkle Tree Construction Algorithm

- **Status**: Accepted
- **Date**: 2025-11-25

## Context and Problem Statement

The Staging Area (Index) stores a flat list of file paths and their blob hashes (e.g., `data/train/A.bin`, `src/main.go`). To create a Version Commit, we must transform this flat list into a hierarchical Merkle Tree structure (`core.Tree` objects) that mirrors the directory layout.

Crucially, this transformation must be **deterministic**: the same set of files must _always_ result in the exact same Root Tree Hash, regardless of the order in which they were added to the Index.

## Decision Drivers

- **Determinism**: The Merkle Root is the identity of the version. Non-deterministic tree building would break deduplication and verification.
- **Simplicity**: The algorithm should be easy to verify and debug during the MVP phase.
- **Completeness**: Must handle nested directories of arbitrary depth.

## Considered Options

1.  **Streaming/Iterator Approach**:
    - _Description_: Sort the index first, then iterate linearly, constructing tree nodes on the fly as directory prefixes change.
    - _Pros_: O(1) memory usage (regarding directory depth).
    - _Cons_: Complex state management to handle backtracking and hash propagation.
2.  **In-Memory Trie Construction (Selected)**:
    - _Description_: "Inflate" the full directory structure into an in-memory Trie (Prefix Tree), then collapse it.
    - _Pros_: Logic is straightforward and easy to unit test; handles "inflation" naturally.
    - _Cons_: Memory usage scales linearly with the number of files/directories ($O(N)$).

## Decision Outcome

We decided to implement the **In-Memory Trie + Post-Order Traversal** approach.

### 1. Inflation Phase

We parse all paths from the Index and construct a temporary node tree in memory.

- Path `a/b/c.txt` creates nodes `a` -> `b` -> `c.txt`.

### 2. Sorting (The Determinism Key)

Before calculating the hash of any directory node, we **strictly sort its children by name (lexicographical order)**.

- This solves the issue of Go's random map iteration order.
- This ensures that `Hash([a, b])` is always calculated, never `Hash([b, a])`.

### 3. Collapse Phase (Post-Order)

We use a post-order traversal (bottom-up recursion) to persist the tree:

1.  Visit leaves (Files) -> Retrieve Hash from Index.
2.  Visit distinct nodes (Dirs) -> Collect children hashes -> Create `Tree` object -> **Persist to Store** -> Return new Hash.
3.  Repeat until Root.

## Consequences

### Positive

- **Correctness**: The logic guarantees a canonical Merkle Tree structure compatible with standard definitions.
- **Simplicity**: The recursive implementation is concise (<100 lines).

### Negative

- **Memory Bottleneck**: For datasets with millions of files, the in-memory Trie structure may exhaust RAM. We accept this limit for the MVP and plan to migrate to a Streaming/Iterator approach in Phase 3 if needed.

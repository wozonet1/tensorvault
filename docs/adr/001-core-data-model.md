# ADR-001: Core Object Model and Serialization Protocol

- **Status**: Accepted
- **Date**: 2025-11-23

## Context and Problem Statement

TensorVault aims to provide version control for AI assets (large binary models, datasets). The traditional Git object model treats files as monolithic "Blobs," which presents significant challenges in an AI context:

1.  **Memory Constraints**: Loading a 10GB model into memory as a single object is impractical.
2.  **Deduplication Efficiency**: Git's line-based diffs and delta compression are ineffective for binary data. Changing metadata bytes in a large model header shouldn't require restorage of the entire file.
3.  **Random Access**: We need the ability to read specific parts of a model without retrieving the entire file.

We need a data model that supports structural deduplication, efficient integrity verification, and is optimized for large binary streams. Additionally, we need a serialization format that is compact, deterministic, and binary-friendly.

## Decision Drivers

- Need for structural deduplication (dedup at the chunk level, not file level).
- Need for Merkle proofs/integrity checks.
- Need for efficient storage of binary hashes.
- Interoperability potential with the wider decentralized storage ecosystem (e.g., IPFS/IPLD).

## Considered Options

1.  **Flat List of Hashes**: A simple manifest file listing chunk hashes.
    - _Pros_: Simple to implement.
    - _Cons_: No hierarchy, poor random access for huge files, difficult to represent directory structures efficiently.
2.  **Git-style Tree/Blob**:
    - _Pros_: Familiar.
    - _Cons_: Blobs are too large; delta compression is CPU intensive and weak for binaries.
3.  **Merkle DAG + DAG-CBOR (Selected)**:
    - _Pros_: Native support for structural sharing (deduplication), binary-efficient serialization, deterministic hashing.

## Decision Outcome

We decided to adopt a **Merkle DAG (Directed Acyclic Graph)** model serialized using **DAG-CBOR** (RFC 8949).

### 1. Object Hierarchy

We define four core object types, forming a bottom-up verification chain:

- **L1 Chunk**: The leaf node containing raw binary data (sliced by CDC).
- **L2 FileNode (ADL)**: An Advanced Data Layout node acting as a "glue" layer. It maps a logical file to a sequence of `Chunk` links.
- **L3 Tree**: Represents a directory, mapping filenames to `FileNode` or other `Tree` hashes.
- **L4 Commit**: Represents a version snapshot, pointing to a root `Tree` and containing author/message metadata.

### 2. Serialization Protocol

We chose **DAG-CBOR** over JSON or Protobuf:

- **Binary Native**: No need for Base64 encoding for byte arrays (hashes/data), reducing storage overhead.
- **Canonical Encoding**: Enforced map key sorting (`SortCanonical`) ensures that the same object always results in the exact same SHA-256 hash.
- **Link Semantics (Tag 42)**: We utilize CBOR Tag 42 to explicitly distinguish "Links" (CIDs) from normal strings. This allows for future traversal optimizations and IPLD compatibility.

## Consequences

### Positive

- **Granular Deduplication**: If two models share common layers (chunks), they are physically stored only once.
- **Integrity**: The root hash (Commit CID) mathematically guarantees the integrity of the entire project history.
- **Performance**: Small binary size for metadata objects due to CBOR compactness.

### Negative

- **Complexity**: Reading a file requires traversing a graph (Commit -> Tree -> FileNode -> Chunks) rather than reading a single blob.
- **Tooling Overhead**: Debugging CBOR requires specialized tools (can't just `cat` the object file), unlike JSON. (Mitigated by our `tv cat` command).

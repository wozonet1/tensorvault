# ADR-002: Content-Defined Chunking Strategy (FastCDC)

- **Status**: Accepted
- **Date**: 2025-11-23

## Context and Problem Statement

TensorVault is designed to manage large-scale AI assets (models, weights, datasets). These binary files often undergo specific patterns of modification:

1.  **Fine-tuning**: Only a subset of layers/weights change.
2.  **Metadata Updates**: Header information is modified (bytes inserted/deleted at the beginning).
3.  **Dataset Appends**: New records are added to the end or middle of a large dataset.

Standard **Fixed-Size Chunking** (splitting a file every N bytes) is extremely fragile. A single byte insertion at the start of a 10GB file shifts all subsequent boundaries, causing the entire file to be re-uploaded as new chunks. This results in near-zero deduplication efficiency for common AI workflows.

We need a chunking strategy that is **shift-resistant** (changes remain local) and computationally efficient enough to handle GB/s throughput.

## Decision Drivers

- **Deduplication Ratio**: Must handle insertions/deletions without cascading changes.
- **Throughput**: The hashing/chunking algorithm must not become the bottleneck during ingestion.
- **Distribution**: Chunk sizes should be relatively uniform to avoid "pathological cases" (tiny chunks that increase metadata overhead or huge chunks that reduce deduplication).

## Considered Options

1.  **Fixed-Size Chunking**:
    - _Pros_: Zero CPU overhead (simple arithmetic).
    - _Cons_: Catastrophic failure on data shifts. Zero deduplication for prepended data.
2.  **Rabin Fingerprinting (Classic CDC)**:
    - _Pros_: Standard shift-resistant algorithm used in systems like LBFS.
    - _Cons_: Rolling hash calculation (polynomial arithmetic) is computationally expensive and slow in software implementations.
3.  **FastCDC (Gear Hash + Normalization) (Selected)**:
    - _Pros_: Uses simplified "Gear Hash" (table lookups + bitwise ops) which is 3x-10x faster than Rabin. Includes "Normalization" to ensure uniform chunk size distribution.

## Decision Outcome

We decided to implement **FastCDC (Fast Content-Defined Chunking)** as our core splitting algorithm.

### 1. Algorithm Specifics

- **Gear Hash**: We utilize a pre-computed 256-entry Gear Table for rapid rolling hash calculation.
- **Sub-minimum Masking**: To further speed up processing, we skip hashing for the `MinSize` bytes of every chunk (since a chunk cannot be smaller than `MinSize`).
- **Normalization**: We employ the "Normalized Chunking" (NC) level 2 strategy. This dynamically adjusts the cut mask based on the current chunk size, significantly reducing the probability of very small or very large chunks.

### 2. Configuration Parameters (Current Defaults)

- **MinSize**: 4 KB
- **AvgSize**: 8 KB
- **MaxSize**: 64 KB
- _Note_: These defaults are chosen for granular deduplication during the MVP phase. In production for TB-scale models, we may increase `AvgSize` to 1MB-4MB to reduce the total number of chunks and metadata overhead.

## Consequences

### Positive

- **Shift Resistance**: Modifying the header of a model only affects the first chunk; the remaining chunks are recognized as duplicates and skipped.
- **Performance**: FastCDC is highly optimized for modern CPUs, allowing chunking speeds that can saturate disk I/O.
- **Storage Efficiency**: Significant reduction in physical storage usage for versioned datasets and fine-tuned models.

### Negative

- **CPU Overhead**: While faster than Rabin, FastCDC still burns CPU cycles compared to fixed-size chunking.
- **Variable Latency**: Chunk sizes are unpredictable (probabilistic), complicating seek logic (offset calculation requires traversing the `FileNode` index).

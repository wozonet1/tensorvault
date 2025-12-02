# ADR-011: Concurrent Ingestion Pipeline and Streaming Reassembly

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

The initial implementation of the `Ingester` processed files serially: reading the entire file into memory, chunking it, and then uploading chunks one by one.

This approach presented two critical bottlenecks:

1.  **Memory Exhaustion (OOM)**: Loading a 10GB model entirely into RAM is not feasible on standard consumer hardware or cloud instances.
2.  **Throughput Latency**: The system was bottlenecked by the round-trip time (RTT) of sequential S3 uploads. CPU (hashing) and Disk IO resources were severely underutilized.

We need an architecture that handles files of arbitrary size (stream processing) and maximizes throughput by parallelizing network operations, while ensuring the final `FileNode` maintains the correct chunk order.

## Decision Drivers

- **Memory Safety**: Memory usage must remain constant regardless of input file size.
- **Performance**: Maximize saturation of network bandwidth via concurrency.
- **Correctness**: The reassembled file must strictly preserve the physical order of chunks, even if workers complete out-of-order.
- **Robustness**: The system must "fail fast" if any single component (Disk read, Hash, Upload) encounters an error.

## Considered Options

1.  **Chunk-then-Upload (Batching)**:

    - _Description_: Read 100MB, chunk it, upload in parallel, repeat.
    - _Pros_: Simpler state management.
    - _Cons_: "Stop-and-wait" behavior reduces throughput; CPU idles while waiting for uploads to finish.

2.  **Pipelined Worker Pool (Selected)**:
    - _Description_: A streaming pipeline with three distinct stages: Generator -> Workers -> Collector.
    - _Pros_: Fully asynchronous; overlaps IO/CPU/Network operations; constant memory footprint.
    - _Cons_: Higher complexity in error handling and reordering.

## Decision Outcome

We decided to implement the **Pipelined Worker Pool** architecture.

### 1. Pipeline Stages

- **Stage 1: Generator (IO-Bound)**:
  - Reads the file in fixed-size buffers (e.g., 1MB).
  - Performs CDC chunking. Handles "remainder" bytes (data cut mid-stream) by carrying them over to the next buffer.
  - Sends raw byte chunks to a `jobs` channel.
- **Stage 2: Workers (CPU/Net-Bound)**:
  - A pool of Goroutines (default: 16) consumes the `jobs` channel.
  - Calculates SHA-256 hash (CPU).
  - Uploads to Storage (Network).
  - Sends metadata (Hash + Size + Index) to a `results` channel.
- **Stage 3: Collector (Memory-Bound)**:
  - Consumes the `results` channel.
  - Reorders the chunks using a Sliding Window algorithm.
  - Assembles the final `FileNode`.

### 2. Concurrency Control

- **Backpressure**: We use **Buffered Channels** (`capacity = WorkerCount * 2`). If workers effectively stall (slow network), the channel fills up, blocking the Generator. This prevents memory explosions by naturally slowing down disk reads to match network speed.
- **Lifecycle Management**: We utilize `golang.org/x/sync/errgroup`. This ensures that if _any_ goroutine returns an error (e.g., S3 Auth Failure), the context is immediately cancelled, stopping all other workers and the generator to save resources ("Fail Fast").

### 3. Reassembly Algorithm

Since workers finish out-of-order, we implement a **Sliding Window Reassembly** mechanism in the Collector:

- Maintain a `nextIndex` pointer (starts at 0) and a `pending` map.
- When a result arrives:
  - If `index == nextIndex`: Commit it, increment `nextIndex`, and check if `nextIndex+1` is already in the map (cascading commit).
  - If `index > nextIndex`: Buffer it in the `pending` map.

## Consequences

### Positive

- **Scalability**: Can ingest Terabyte-scale files with a fixed memory footprint (approx. `WorkerCount * ChunkMaxSize`).
- **Speed**: Upload speed is now limited only by available network bandwidth or S3 limits, not by serial processing latency.
- **Responsiveness**: The system responds to cancellation signals (Context) within milliseconds.

### Negative

- **Complexity**: Debugging race conditions or deadlocks in pipelines is harder than in serial code (mitigated by strictly using `errgroup` and bounded channels).
- **Resource Contention**: High concurrency might saturate file descriptors or network connections if not tuned properly (mitigated by fixed worker pool size).

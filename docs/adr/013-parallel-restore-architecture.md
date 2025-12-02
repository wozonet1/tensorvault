# ADR-013: Parallel Restore Architecture via WriterAt

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

Recovering large files (e.g., 10GB models) from the repository was initially implemented as a serial process: iterate through the chunk list, download chunk $N$, write to stream, then download chunk $N+1$.

In high-latency network environments (like S3), this serial "Stop-and-Wait" approach severely underutilizes available bandwidth. Since we possess the full metadata (`FileNode`) containing the size of every chunk, we theoretically know the exact physical location (offset) of every byte before downloading it.

We need a mechanism to download chunks in parallel and write them out-of-order to their correct positions, without breaking the support for streaming outputs (like `stdout`) where random access is impossible.

## Decision Drivers

- **Throughput**: Restore speed should be limited only by network/disk bandwidth, not by RTT latency.
- **Compatibility**: The system must still support piping output to standard output (e.g., `tv cat ... | tar x`).
- **Simplicity**: Avoid complex reassembly buffers in memory if the underlying storage supports random access.

## Decision Outcome

We decided to implement a **Polymorphic Dispatch Strategy** in the `Exporter`.

### 1. Interface Detection (`io.WriterAt`)

Go's standard library provides the `io.WriterAt` interface:

```go
type WriterAt interface {
    WriteAt(p []byte, off int64) (n int, err error)
}
```

The `Exporter` checks the provided `io.Writer` at runtime:

- **If it implements `io.WriterAt`** (e.g., `*os.File`): We switch to **Concurrent Mode**.
- **If not** (e.g., `os.Stdout`, `gzip.Writer`): We fallback to **Serial Mode**.

### 2. Concurrent Mode Architecture

- **Generator**: Iterates through the `FileNode` chunk list _once_ to calculate the absolute `offset` for every chunk. It dispatches lightweight `restoreJob` structs (Hash + Offset + Size) to a channel.
- **Workers**: A pool of goroutines consumes jobs. Each worker downloads the chunk data and immediately calls `writer.WriteAt(data, offset)`. Since file systems handle concurrent writes to non-overlapping regions efficiently, no application-level locking is required.

## Consequences

### Positive

- **Performance**: Restore speeds for files saturate disk I/O (e.g., ~800MB/s on local NVMe), as network latency is hidden by concurrency.
- **Usability**: The `tv cat -o file` command automatically benefits from acceleration, while `tv cat > file` remains functional (though slower).
- **Memory Efficiency**: No need to buffer out-of-order chunks in RAM; they are flushed to disk immediately.

### Negative

- **Sparse File Creation**: Writing at high offsets initially might trigger sparse file creation on some filesystems, though modern filesystems handle this well.

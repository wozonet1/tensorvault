# ADR-007: Version Control State Machine (Snapshot Model)

- **Status**: Accepted
- **Date**: 2025-11-25

## Context and Problem Statement

A Version Control System involves transitions between three states: The Working Directory (Disk), The Staging Area (Index), and The Repository (Commit History).

We need to define how `commit` and `checkout` operations affect the Staging Area. Git uses a complex "Continuous Index" model where the Index is rarely cleared, serving as a cache for `stat` calls to speed up `git status`.

For the TensorVault MVP, implementing a Git-like diff engine and high-performance status checker is out of scope. We need a simpler mental model that ensures data consistency.

## Decision Drivers

- **Simplicity**: Reduce the complexity of calculating "what has changed."
- **Safety**: Minimize the risk of "ghost files" (deleted files reappearing).
- **Consistency**: The system state after a `checkout` must be predictable.

## Decision Outcome

We decided to adopt a simplified **"Commit-and-Clear"** state model.

### 1. Commit Behavior

Upon a successful `commit`:

- The Merkle Tree is built and stored.
- **The Index is Reset (Cleared)**.
- _Rationale_: This treats the Index as a "bucket of pending changes." Once changes are committed, the bucket is empty. This simplifies `tv status` (if Index is not empty, there are changes).

### 2. Checkout Behavior

Upon `checkout <commit>`:

- **Hard Reset**: Files in the working directory are overwritten.
- **Index Refill**: The Index is strictly populated with the contents of the target Commit.
- _Rationale_: This ensures that immediately after a checkout, the system is in a "Clean" state (Working Dir == Index == HEAD).

### 3. Smart Add

To support this model, `tv add` is enhanced to perform **Synchronization**:

- It updates existing files.
- It adds new files.
- **It prunes (removes) files** from the Index if they are within the target path but missing from the disk.

## Consequences

### Positive

- **Logical Clarity**: The state transitions are easy to reason about and debug.
- **Code Simplicity**: Avoiding complex "three-way diffs" between Disk/Index/HEAD.

### Negative

- **Performance Cost**: Since the Index is cleared on commit, a subsequent `tv add .` must re-hash files to populate the Index again (lacking the mtime caching optimization of Git).
- **Redundant Commits**: If a user runs `checkout` and immediately `commit`, an identical commit might be generated (deduplication handles the data, but the Commit object is new).

_Note: This model is transitional. As we scale to support massive file counts, we will likely migrate to a Git-like persistent index to optimize `status` and `add` performance._

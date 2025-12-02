# ADR-009: Concurrency Control via Optimistic Locking (CAS)

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

In a distributed version control system like TensorVault, multiple users or automated pipelines may attempt to update the same reference (e.g., pushing to the `main` branch or updating `HEAD`) simultaneously.

Without a concurrency control mechanism, the "Lost Update" problem occurs:

1. User A reads `HEAD` (points to Commit X).
2. User B reads `HEAD` (points to Commit X).
3. User A updates `HEAD` to Commit Y.
4. User B updates `HEAD` to Commit Z.

**Result**: User A's update to Commit Y is silently overwritten and lost. The history becomes linear but incorrect, severing the link to Commit Y.

We need a mechanism to ensure that a reference update is only accepted if the reference hasn't changed since it was last read.

## Decision Drivers

- **Data Integrity**: History must never be lost due to race conditions.
- **Performance**: The solution should minimize database contention and locking overhead.
- **Statelessness**: The solution should not rely on stateful locks in the application layer, as the CLI/Server instances are stateless.
- **Infrastructure Simplicity**: Avoid introducing new coordination services (like Zookeeper or Redis locks) just for this feature.

## Considered Options

1.  **Pessimistic Locking (`SELECT FOR UPDATE`)**:

    - _Description_: Lock the row in the database transaction before reading and updating.
    - _Pros_: Guarantees serialization.
    - _Cons_: Holds database connections open longer; reduces throughput; risk of deadlocks; database-specific syntax differences.

2.  **Distributed Lock Manager (Redis/Etcd)**:

    - _Description_: Acquire a named lock in Redis before performing the DB update.
    - _Pros_: Very robust for high-contention scenarios.
    - _Cons_: Adds significant operational complexity (maintenance of Redis/Etcd cluster); overkill for typical VCS contention levels.

3.  **Optimistic Locking (Compare-And-Swap) [Selected]**:
    - _Description_: Add a `version` column to the database row. Updates are conditional: "Update only if version matches what I read".
    - _Pros_: Non-blocking; works with standard SQL; zero extra infrastructure; high performance when contention is low (typical for Git-like workflows).
    - _Cons_: Requires the client to handle failures (e.g., re-pull and push).

## Decision Outcome

We decided to implement **Optimistic Locking** using a `Version` (int64) field in the `Ref` model.

### 1. Schema Change

The `refs` table in PostgreSQL includes a `version` column, initialized to 1.

### 2. The CAS Algorithm

The update logic in `pkg/meta/repository.go` follows this pattern:

```go
// SQL: UPDATE refs SET hash=?, version=version+1 WHERE name=? AND version=?
result := db.Model(&Ref{}).
    Where("name = ? AND version = ?", refName, oldVersion).
    Updates(map[string]interface{}{
        "commit_hash": newHash,
        "version":     gorm.Expr("version + 1"),
    })

if result.RowsAffected == 0 {
    return ErrConcurrentUpdate // The CAS operation failed
}
```

### 3. Handling Creation Races (Insert-if-Not-Exists)

For the scenario where two users try to create a _new_ branch simultaneously:

- We rely on the databases **Unique Constraint** on the `name` column.
- We catch `ErrDuplicatedKey` (or database-specific unique constraint errors) and map them to `ErrConcurrentUpdate`.

## Consequences

### Positive

- **Safety**: Impossible to overwrite a branch update accidentally.
- **Simplicity**: The logic is contained entirely within the SQL update statement.
- **Scalability**: No database locks are held during the "think time" (generating the commit object), only during the brief execution of the update statement.

### Negative

- **User Experience Friction**: Users (or API clients) must handle the `ErrConcurrentUpdate` error. In the CLI, this typically manifests as a message: _"Remote contains work that you do not have. Please pull and try again."_

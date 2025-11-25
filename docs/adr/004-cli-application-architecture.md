### `docs/adr/0004-cli-application-architecture.md`

# ADR-004: CLI Application Architecture and Dependency Injection

- **Status**: Accepted
- **Date**: 2025-11-25

## Context and Problem Statement

In the initial implementation of the TensorVault CLI, individual commands (e.g., `tv add`, `tv cat`) were responsible for instantiating their own infrastructure dependencies (e.g., creating the `DiskStore` adapter directly).

This "hardcoded instantiation" pattern led to several critical architectural issues:

1.  **Configuration Bypass**: Commands ignored the global configuration managed by Viper (e.g., user-defined `--storage-path`), creating storage instances in hardcoded default locations instead.
2.  **Testing Difficulty**: It was impossible to inject mock stores or in-memory indexes during unit testing, as the dependency creation was buried inside the command's `Run` function.
3.  **Code Duplication**: Every command repeated the initialization logic for storage and configuration, violating the DRY principle.

We need a unified architectural pattern to manage the application lifecycle and dependency wiring (Dependency Injection).

## Decision Drivers

- **Configuration Consistency**: The application must strictly respect the precedence of Flags > Env > Config File > Defaults.
- **Testability**: Infrastructure dependencies (Storage, Index) must be decoupled from UI logic (CLI commands) to allow mocking.
- **Maintainability**: Initialization logic should exist in one single source of truth.

## Considered Options

1.  **Global Singleton Variables (Anti-Pattern)**:
    - _Description_: Relying on package-level global variables initialized in `init()`.
    - _Pros_: Easiest to access.
    - _Cons_: Initialization order is brittle; side effects make parallel testing impossible; hard to reset state.
2.  **Reflection-based DI Frameworks (Uber Dig / Google Wire)**:
    - _Description_: Using heavy frameworks to resolve the dependency graph automatically.
    - _Pros_: Automatic resolution.
    - _Cons_: Overkill for a CLI application; adds steep learning curve, "magic," and runtime reflection overhead (Dig) or code generation steps (Wire).
3.  **Manual Dependency Injection Container (Selected)**:
    - _Description_: Defining a struct (`App`) that holds all dependencies and passing it explicitly.
    - _Pros_: Explicit, simple, transparent, and compile-time safe. No magic.

## Decision Outcome

We decided to implement a **Manual Dependency Injection** pattern using a lightweight **Application Container**.

### 1. The App Container (`pkg/app`)

We introduce a dedicated package `pkg/app` to define the `App` struct. This struct acts as a container for all long-lived service instances (Singleton-like objects):

```go
type App struct {
    Store storage.Store
    Index *index.Index
    // Future: Logger, Config, etc.
}
```

### 2. The Factory Pattern

We implemented `app.NewApp()` as the **Single Source of Truth** for initialization. This factory:

- Reads configuration values explicitly from Viper.
- Instantiates the concrete `Store` implementation (currently DiskAdapter, swappable for S3Adapter in the future).
- Loads the `Index` state from the filesystem.
- Returns a fully assembled `App` instance or a unified error.

### 3. Lifecycle Integration

We integrated this into the Cobra lifecycle using the `PersistentPreRunE` hook in the root command (`cmd/tv/root.go`). This ensures that dependencies are wired, validated, and ready _before_ any business logic executes.

## Consequences

### Positive

- **Config Obedience**: All commands now correctly use the configured storage path (e.g., from `config.yaml` or env vars).
- **Testability**: We can now write integration tests by constructing an `App` with an `InMemoryStore` and passing it directly to command handlers.
- **Clean Architecture**: The Interface Layer (CLI commands) is strictly decoupled from the Infrastructure Layer (`storage/disk`).

### Negative

- **Boilerplate**: Requires a small amount of setup code in `root.go` and `pkg/app` compared to the script-style approach.
- **Global State (Pragmatic Compromise)**: To integrate with Cobra's command structure without passing Context everywhere (which can be verbose in simple CLIs), we currently assign the initialized `App` to a package-level variable `TV` in `root.go`. While not "pure" DI, this is an accepted pattern for CLI tools.

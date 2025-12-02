# ADR-010: Domain Primitives Enforcement (Strong Typing)

- **Status**: Accepted
- **Date**: 2025-12-02

## Context and Problem Statement

In the early stages of development, we used raw Go `string` types to represent core domain concepts such as **Object Hashes (Content-IDs)** and **Repository Paths**.

This "Stringly-typed" approach led to several architectural risks:

1.  **Ambiguity**: A function signature like `func Put(ctx context.Context, id string, path string)` makes it easy to accidentally swap arguments.
2.  **Lack of Validation**: A raw `string` variable offers no guarantee that it contains a valid SHA-256 hash. Validation logic was scattered across `core`, `storage`, and `cmd` packages.
3.  **Semantic Loss**: It was unclear whether a string variable represented a full, valid Hash or a user-provided short prefix (search term).

We need a way to enforce domain rules at the type system level, catching logic errors at compile time rather than runtime.

## Decision Drivers

- **Type Safety**: Leveraging Go's static typing to prevent illegal states.
- **Readability**: Function signatures should clearly communicate intent (e.g., `Get(Hash)` vs `Get(string)`).
- **Logic Centralization**: Validation rules (e.g., "Must be 64-char Hex") should live with the type definition.
- **Refactoring Safety**: Future changes to the underlying ID format (e.g., switching to binary `[32]byte` for memory optimization) should be manageable.

## Considered Options

1.  **Raw Strings (Status Quo)**:

    - _Pros_: Zero boilerplate; zero conversion overhead; fully compatible with standard library interfaces.
    - _Cons_: High risk of "primitive obsession" anti-pattern; bugs discovered late.

2.  **Struct Wrappers** (e.g., `type Hash struct { value string }`):

    - _Pros_: Prevents accidental casting; strictly distinct types.
    - _Cons_: High friction usage; requires unwrapping (`h.value`) everywhere; JSON marshalling requires custom boilerplate.

3.  **Type Aliases/Definitions** (e.g., `type Hash string`) **[Selected]**:
    - _Pros_: Lightweight; methods can be attached (`h.IsValid()`); easy conversion (`string(h)`); JSON compatible.
    - _Cons_: Requires explicit conversion at I/O boundaries.

## Decision Outcome

We decided to introduce a dedicated **`pkg/types`** package and adopt **Domain Primitives** (Type Definitions) for core concepts.

### 1. The `Hash` Primitive

We defined `type Hash string` as the canonical representation of an Object ID.

- **Invariant**: A `Hash` instance is expected to be a valid 64-character hexadecimal string.
- **Helper**: Added `Hash.IsValid()` and `Hash.String()` methods.

### 2. The `HashPrefix` Primitive

We introduced `type HashPrefix string` to explicitly distinguish "User Input / Search Query" from "Valid Object ID".

- **Flow**: The CLI accepts a `HashPrefix`. The Storage layer's `ExpandHash` accepts a `HashPrefix` and returns a `Hash`. This makes the transition from "fuzzy" to "exact" explicit.

### 3. Interface Updates

Core interfaces were updated to enforce these types:

- `core.Object.ID()` now returns `types.Hash`.
- `storage.Store.Get()` now accepts `types.Hash`.

## Consequences

### Positive

- **Compile-Time Safety**: Swapping a path and a hash is now a compilation error.
- **Self-Documenting Code**: Signatures like `func GetCommit(h types.Hash)` are self-explanatory.
- **Centralized Validation**: Validation logic is now encapsulated in `pkg/types`.

### Negative

- **I/O Boundary Friction**: Standard libraries (e.g., `os`, `io`) and external SDKs (e.g., `aws-sdk-go-v2`) only accept `string`. We must perform explicit conversions (e.g., `aws.String(string(h))`) at the edges of the system (Adapters).
- **Test Ripple Effect**: All mock data generation in tests required updating to return typed values instead of raw strings.

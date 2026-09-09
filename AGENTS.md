# AGENTS.md

This document defines the architecture, coding, testing, and agent-behavior requirements for this project.

## 1. Instruction Precedence

When instructions conflict, agents must follow the highest-priority applicable instruction from this precedence order:

1. **Pull request review comments**
2. **Ticket/task requirements**
3. `AGENTS.md` requirements
4. Existing project conventions and patterns

- Pull request review comments may override or supersede any instruction defined in `AGENTS.md`.
- Ticket requirements may override or supersede `AGENTS.md` requirements when they explicitly require different behavior.
- Agents must follow the highest-priority applicable instruction when requirements conflict.

## 2. Project Context

This project is an **SDLC Orchestration Engine** implemented using **Vertical Slice Architecture**.

The engine is intentionally integration-agnostic and is designed to integrate with external platforms and tools such as:

- Jira
- Linear
- Asana
- GitHub
- Daytona
- OpenCode
- Codex
- Claude Code

The `features` directory contains the business capabilities of the engine.

The architecture uses interfaces to allow capabilities to have interchangeable implementations and provide a plug-and-play integration model.

The core engine should not be coupled to a specific external vendor or platform.

## 3. Agent Responsibilities and Scope

Agents must remain focused on **implementing the requested functionality**.

Agents must:

- Implement the requirements specified by the ticket.
- Follow the project's existing architecture and conventions.
- Keep changes focused on the requested functionality.
- Add or modify tests when appropriate.
- Avoid unrelated refactoring.
- Avoid introducing abstractions that are not required by the implementation.
- Only push changes when explicitly requested.
- When requested to push changes, push the implementation so it can proceed through pull request review.

Agents should not independently expand the scope of the task.

## 4. Go Tooling Restrictions

Agents must **never install, download, configure, or execute Go tooling**.

This includes, but is not limited to:

- Downloading or installing Go.
- Running `go build`.
- Running `go test`.
- Running `go vet`.
- Running `go fmt`.
- Running `go generate`.
- Running any other `go` command.
- Installing Go through a package manager.
- Downloading a Go binary or toolchain as a workaround.

This restriction applies regardless of whether the agent believes running the command would be useful for validation.

If validation would normally require a Go command, the agent must not attempt to work around this restriction.

## 5. Feature and Plug-in Structure

The standard structure for a feature or plug-in is:

```
features/<feature_name>/
    controller.go
    interfaces/
        repository.go
        repository_[n].go
        handler_[n].go
    types/
        types.go
        errors.go
    infrastructure/
        postgres.go
        client.go
    metrics/
        metric_[n].go

plug-ins/<plug-in_name>/
    handler_[n].go
    interfaces/
        [interface_name].go
    types/
        types.go
        errors.go
    infrastructure/
        [interface_implementation].go
    metrics/
        metric_[n].go
```

The exact files may vary depending on the feature or plug-in, but agents should follow this structure and the established patterns in the repository.

Plug-ins MUST never define repositories.

The plug-in main entry point must be named exactly as the interface they are implementing. For example, if a feature defines an interface named `handler_webhook.go`, then the plug-in that implements it must also create a file named `handler_webhook.go`.

## 6. Features

Features represent the **business capabilities of the SDLC Orchestration Engine**.

A feature should contain the code required to implement and expose its capability while maintaining clear boundaries from other features.

### Controller

`controller.go` contains the primary orchestration/entry point for the feature.

Business behavior should remain cohesive within the feature.

### Interfaces

The `interfaces` directory contains the interfaces required by the feature.

This includes:

- The feature's primary repository interface.
- Interfaces representing repositories belonging to other features when the feature needs to consume that capability without creating a direct dependency.
- Interfaces required to support plug-and-play implementations.
- Interfaces used to abstract infrastructure dependencies.

#### Cross-feature repository interfaces

A feature may define an interface such as:

```
interfaces/repository_x[n].go
```

when it needs to consume functionality that conceptually belongs to another feature.

The purpose is to prevent the feature from depending directly on another feature's concrete implementation.

The implementation of that interface may exist in multiple repositories or implementations elsewhere in the system.

Agents must preserve this architectural boundary rather than creating direct feature-to-feature implementation dependencies.

## 7. Types

Types belong to the feature or plug-in that owns the concept.

The standard location is:

```
types/types.go
types/errors.go
```

Types must be placed in the `types` directory of the corresponding feature or plug-in.

Do not create unnecessary global or shared types.

### Interface-specific exception

The only exception is a type that belongs **exclusively to an interface definition that represents a plug-in implementation**.

These interfaces are contracts defined by a feature specifically to enable plug-ins to provide interchangeable implementations.

When a type exists solely to define the contract between a feature and its plug-in implementations, that type may be defined alongside the interface rather than in the feature's `types` directory.

This exception applies **only** to interfaces defined inside `features` whose purpose is to be implemented by plug-ins.

For example, if a feature defines an interface that plug-ins must implement, types that are exclusively part of that plug-in contract may live with the interface.

This exception does **not** apply to:

- General feature types.
- Domain types.
- Types used by multiple parts of a feature.
- Repository types that are not exclusively part of a plug-in contract.
- Infrastructure types.
- Types belonging to a plug-in's internal implementation.
- Interfaces created solely to facilitate testing or abstract an infrastructure dependency.

The ownership rule should remain:

> A type belongs in the `types` directory of the feature or plug-in that owns the concept, unless the type exists exclusively as part of a feature-defined interface contract that is implemented by plug-ins.

## 8. Infrastructure

Each feature or plug-in may contain an `infrastructure` directory.

Infrastructure implementations provide concrete implementations of the interfaces defined by the feature or plug-in.

For example:

```
infrastructure/
    postgres.go
    client.go
```

where:

- `postgres.go` implements repository interfaces.
- `client.go` implements an interface used to communicate with an external system or third-party service.

Infrastructure code should remain behind the appropriate interface boundary.

### Root `infrastructure` Directory

There is also an `infrastructure` directory at the project level.

**Agents must not modify, create, remove, or otherwise work in the root `infrastructure` directory** unless explicitly required by the ticket requirements or requested during pull request review.

The existence of an infrastructure-related task should not be assumed merely because an implementation touches infrastructure.

Do not proactively refactor or reorganize the root `infrastructure` directory.

## 9. Plug-ins

Plug-ins are located under:

```
plug-ins/<plug-in_name>
```

Plug-ins provide concrete implementations of capabilities that allow the engine to integrate with external systems.

Plug-ins must be **self-contained**.

All plug-in-specific concerns should remain inside the plug-in directory, including where applicable:

- Dependencies.
- External SDKs.
- Infrastructure.
- Third-party clients.
- Types.
- Interfaces.
- Metrics.
- Tests.
- Adapters.
- Configuration specific to the plug-in.

### Plug-in Removability

A plug-in must be independently removable.

If a plug-in directory is completely removed from the source code, the rest of the project must not be negatively affected.

Agents must therefore avoid:

- Placing plug-in-specific implementation details in shared code.
- Creating unnecessary dependencies from features to plug-ins.
- Coupling the core engine directly to a specific vendor.
- Requiring unrelated features to change when a plug-in is removed.
- Defining or implementing repositories in plug-ins.

The intended architecture is:

**Feature defines capability → plug-in provides implementation.**

This enables integrations to be added, replaced, or removed without changing the core business logic.

## 10. Plug-and-Play Interfaces

Features may define interfaces to enable multiple interchangeable implementations.

These interfaces may represent:

- External integrations.
- Repositories.
- Infrastructure dependencies.
- Other interchangeable capabilities.

Agents should use the existing interface boundaries rather than introducing direct dependencies on concrete implementations.

Do not introduce an interface solely for the sake of abstraction when there is no meaningful architectural or testing boundary.

## 11. Metrics

Feature- and plug-in-specific metrics belong in the component's `metrics` directory.

For example:

```
metrics/
    metric_[n].go
```

Metrics implemented for a feature should remain owned by that feature.

Metrics specific to a plug-in should remain inside the plug-in.

Do not move component-specific metrics into shared locations unless explicitly required.

## 12. Business Logic

Business logic should remain cohesive and easy to follow.

Do not split a single business operation into multiple functions merely to make functions smaller.

Extract business logic into separate functions only when:

- The function is reused by multiple callers, or
- It represents a genuinely independent operation that benefits from being separated.

Avoid unnecessary helper functions, excessive indirection, and abstractions that make the business flow harder to understand.

Prefer straightforward, cohesive implementations.

## 13. Comments

Keep comments to a minimum.

Only add comments when the logic is sufficiently complex or non-obvious that its intention cannot be easily understood from the code.

Comments should explain **why** something exists or why a particular approach was necessary.

Comments should not describe the code line by line or restate what the implementation obviously does.

Prefer clear naming and straightforward code over explanatory comments.

## 14. Testing

When implementing tests, agents should aim for **100% coverage of the relevant code whenever practical**. Tests must be implemented using testify with the `suite.Suite` style.

Tests must verify behavior rather than simply execute code.

Tests should assert the expected:

- Inputs.
- Outputs.
- State changes.
- Errors.
- Important edge cases.

Tests must provide meaningful assertions that confirm the implementation behaves according to the requirements.

Avoid tests that only confirm that a function executes successfully without validating its behavior.

### External SDK / HTTP Requests

Do not write tests for code that directly makes HTTP requests through an external SDK.

This will commonly apply to integration code implemented within plug-ins.

The purpose of this rule is to avoid testing the behavior of external SDKs themselves.

Tests should focus on the application's own behavior and logic surrounding the integration where appropriate.

## 15. OpenTelemetry in Tests

The engine supports **native OpenTelemetry instrumentation**.

The OpenTelemetry Go SDK does not have an operational destination/export flow in the test environment.

Therefore, when writing tests:

- Do not add special handling solely to determine where OpenTelemetry data is being exported.
- Do not introduce test-specific infrastructure for OpenTelemetry unless explicitly required.
- Do not treat the absence of an operational OpenTelemetry destination in tests as a problem.
- Tests may execute instrumented code without needing to verify where the telemetry data is sent.

The existing OpenTelemetry instrumentation should not become a concern of individual test cases unless the ticket explicitly requires testing telemetry behavior.

## 16. Error Logging

The component that **produces an error is responsible for logging that error**.

If a feature produces an error, the feature should log it.

If a plug-in produces an error, the plug-in should log it.

Higher-level components should not unnecessarily log the same error again as it propagates through the system.

Avoid duplicate logging of the same error at multiple architectural boundaries.

## 17. Log Prefixes

Every log message must include a component prefix.

The format is:

```
[component] message
```

The component must identify the **feature or plug-in responsible for producing the log**.

Examples:

```
[agent_session] failed to create agent session
[jira] failed to update issue
[github] failed to create pull request
```

Use the feature or plug-in name as the component identifier.

Avoid generic prefixes such as:

```
[service]
[handler]
[manager]
[repository]
```

when the owning feature or plug-in can be identified.

The prefix must make it clear which feature or plug-in produced the log.

The `[service]` prefix is reserved for the `main.go` file where the service itself is configured.

## 18. `features/agent_session` as an Architectural Reference

The `features/agent_session` feature should be treated as the primary reference for the project's established feature architecture and coding style.

When implementing or modifying features, agents should use it to understand:

- Directory organization.
- Interface placement.
- Type placement.
- Controller responsibilities.
- Infrastructure boundaries.
- Business logic organization.
- Naming conventions.
- General coding style.

Agents should follow the established patterns rather than introducing a different architectural style.

However, `features/agent_session` should be used as an architectural reference, not blindly copied. Existing patterns should be adapted to the requirements of the feature being implemented.

## 19. Minimal and Focused Changes

Agents should make the smallest reasonable change necessary to satisfy the requirements.

Do not:

- Refactor unrelated code.
- Reorganize directories without a requirement.
- Introduce new architectural patterns unnecessarily.
- Move existing code solely because it could theoretically be structured differently.
- Modify the root `infrastructure` directory without explicit authorization.
- Modify unrelated features or plug-ins.

Architectural improvements that are not necessary for the requested task should generally be left for a separate task.

## 20. Pull Request Workflow

Agents must not assume that they should push changes after implementation.

Only push changes when explicitly requested.

Once a pull request is under review, **review comments take precedence over the existing `AGENTS.md` instructions** when they conflict.

Agents should treat review feedback as an authoritative refinement of the implementation requirements and modify the implementation accordingly.

The same precedence applies even when the requested change conflicts with an existing architectural guideline in `AGENTS.md`.

The goal of `AGENTS.md` is to establish the project's default engineering rules, not to prevent explicit requirements or review decisions from overriding those defaults.
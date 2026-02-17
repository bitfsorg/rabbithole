<!--
Sync Impact Report
==================
Version change: N/A → 1.0.0 (initial ratification)
Modified principles: N/A (new document)
Added sections:
  - Core Principles (3 principles)
  - Development Workflow
  - Quality Gates
  - Governance
Removed sections: N/A
Templates requiring updates:
  - .specify/templates/plan-template.md ✅ compatible (Constitution Check section aligns)
  - .specify/templates/spec-template.md ✅ compatible (user stories + acceptance scenarios align)
  - .specify/templates/tasks-template.md ✅ compatible (test-first ordering enforced)
Follow-up TODOs: None
-->

# BitFS Constitution

## Core Principles

### I. Plan First

Every feature, module, or significant change MUST begin with a written plan
before any code is written. Plans MUST include:

- Clear problem statement and scope definition
- Technical approach with rationale for key decisions
- Identified dependencies, risks, and constraints
- File paths and module boundaries affected

**Rationale**: Upfront planning prevents wasted implementation effort,
surfaces design conflicts early, and ensures alignment between
collaborators before code is committed.

### II. Documentation First

Design documents and specifications MUST precede implementation code.
The documentation chain follows this order:

1. Spec (`spec.md`) — defines what to build (user stories, requirements)
2. Plan (`plan.md`) — defines how to build it (architecture, structure)
3. Tasks (`tasks.md`) — defines the execution order
4. Code — implements what the documents specify

Documentation MUST be the source of truth. When code and documentation
diverge, documentation MUST be updated first, then code adjusted to match.
Generated outputs (PDFs, HTML) MUST NOT be edited directly; modify the
Markdown source and regenerate.

**Rationale**: Documentation-first ensures shared understanding, enables
review before investment, and creates a traceable record of design
decisions.

### III. Test-Driven Development (NON-NEGOTIABLE)

All implementation MUST follow strict TDD discipline. The workflow is:

1. **Write tests first** — test code MUST be written before any
   implementation code for the feature under development
2. **Verify test completeness** — all planned test cases MUST be written
   and confirmed to cover the specification requirements
3. **Tests MUST fail** — run the test suite to confirm tests fail
   (Red phase), proving they test real behavior
4. **User confirmation gate** — the complete test suite MUST be presented
   to and approved by the user before any implementation begins
5. **Implement to pass** — write the minimum implementation code to make
   tests pass (Green phase)
6. **Refactor** — clean up implementation while keeping tests green

Skipping the user confirmation gate is NEVER permitted. Tests that pass
before implementation indicate the test is not testing the right thing
and MUST be rewritten.

**Rationale**: TDD with an explicit approval gate ensures tests reflect
true requirements, prevents implementation from drifting from spec, and
gives the user control over quality standards before code is written.

## Development Workflow

The mandatory workflow for every feature or change:

```
Plan → Document → Test (write) → Test (verify) → User Approval → Implement → Refactor
```

Step-by-step:

1. **Plan**: Create or update the feature plan per Principle I
2. **Document**: Write spec and design docs per Principle II
3. **Write tests**: Author all test cases covering spec requirements
4. **Verify tests**: Run tests to confirm they fail (Red phase)
5. **User approval**: Present test suite for user review and confirmation
6. **Implement**: Write code to make tests pass (Green phase)
7. **Refactor**: Clean up while maintaining passing tests

Gate violations:
- Writing implementation code before tests exist → BLOCKED
- Writing implementation code before user approves tests → BLOCKED
- Skipping the plan/spec phase for non-trivial changes → BLOCKED

## Quality Gates

| Gate | Trigger | Required Action |
|------|---------|-----------------|
| Plan Review | Before any new feature work | Plan document exists and covers scope |
| Spec Review | Before test writing begins | Spec defines user stories + acceptance criteria |
| Test Completeness | Before user approval | All spec requirements have corresponding tests |
| Test Red Phase | Before implementation | All new tests fail, confirming they test real behavior |
| User Approval | Before implementation begins | User explicitly confirms test suite is complete |
| Test Green Phase | After implementation | All tests pass with no skips |
| Refactor Check | After green phase | Tests still pass after cleanup |

## Governance

This constitution supersedes all other development practices for the
BitFS project. Compliance is mandatory for all changes.

**Amendment procedure**:
1. Propose amendment with rationale
2. Document the change with before/after comparison
3. Update version per semantic versioning (below)
4. Propagate changes to dependent templates

**Versioning policy**:
- MAJOR: Principle removed, redefined, or made backward-incompatible
- MINOR: New principle or section added, or existing guidance materially expanded
- PATCH: Wording clarifications, typo fixes, non-semantic refinements

**Compliance review**:
- All code changes MUST demonstrate adherence to the three core principles
- The Plan → Document → Test → Approve → Implement workflow is non-negotiable
- Constitution violations MUST be flagged and resolved before merge

**Version**: 1.0.0 | **Ratified**: 2026-02-17 | **Last Amended**: 2026-02-17

# Finance Agent Onboarding Guide

This guide helps AI agents and developers set up, develop, and navigate the
`finance` repository: `github.com/Toshik1978/finance`, a single Go module
providing a currency/quote CLI and library.

---

## CLI Command Reference

All automation is managed via `go-task` (`Taskfile.yml`).

* Download Go module dependencies:
  ```bash
  task setup
  ```
* Run golangci-lint (the same gate CI enforces):
  ```bash
  task lint
  ```
* Auto-format the codebase:
  ```bash
  task format
  ```
* Run unit tests:
  ```bash
  task test
  ```
* Run tests with the race detector and coverage over the gate scope:
  ```bash
  task test:coverage
  ```
* Compile the `finance` binary:
  ```bash
  task build
  ```
* Remove build artifacts and test cache:
  ```bash
  task clean
  ```

---

## Task Rules

These rules apply to every task. Non-negotiable.

1. **Third-Party Dependencies Require Approval**: Before introducing any
   external dependency, state the package, what it solves, and why the
   standard library is insufficient, and get explicit approval before adding
   it.

   **Approved dependencies (recorded):**
   * `github.com/shopspring/decimal`, pinned at `v1.4.0` — the only runtime
     dependency the library packages (`finance`, `civil`, `provider/...`,
     `storage/...`) may import. Do not track its master branch.
   * `github.com/spf13/cobra`, `github.com/fatih/color`,
     `github.com/caarlos0/env/v11`, `github.com/joho/godotenv`,
     `modernc.org/sqlite` — additionally usable in `cmd/finance` and
     `internal/cli`.
   * `github.com/stretchr/testify`, `github.com/jarcoal/httpmock` — additionally
     usable in tests. `storage/sqldb`'s test file may blank-import
     `modernc.org/sqlite` for a real database to exercise; the package itself
     imports only `database/sql`, never a driver.

   None of these may be imported by this module's own code, and none may appear
   in the library packages' runtime graph: `Toshik1978/go-kit`,
   `go.elastic.co/apm/*`, `go.uber.org/zap`, `go.uber.org/fx`, `jmoiron/sqlx`,
   `timshannon/bolthold`, `go.etcd.io/bbolt`, `spf13/viper`, `golang.org/x/text`,
   `google/uuid`, `maxatome/tdhttpmock`, `mattn/go-sqlite3`.

   The one exemption: `google/uuid` arrives transitively through
   `modernc.org/sqlite` -> `modernc.org/libc`, which requires it unconditionally
   across every recent release. Since that driver is mandated, the ban cannot be
   read as covering transitive arrivals. It bans deliberate use — the point was
   never to generate random test fixtures with it. No file in this module imports
   it, and it stays out of the library packages entirely: only `cmd/finance` and
   `storage/sqldb`'s test file reach the driver.

2. **Testing with testify suites**: All Go tests use
   `github.com/stretchr/testify`, organised as suites. Three non-negotiable
   rules:
   1. **One entry point per package** — exactly one top-level
      `func Test<Package>(t *testing.T)`. No other top-level `Test*` function
      may exist in the package, except `func TestMain(m *testing.M)`, which is
      exempt.
   2. **The entry point only wires suites** — it consists solely of
      `suite.Run(t, new(...))` calls, one per `suite.Suite`, and contains no
      test logic itself.
   3. **All real tests are suite methods** — every assertion lives in a
      method on a `suite.Suite`, using suite assertion methods (`s.Equal`,
      `s.Require().NoError`, …). Never a bare `func TestX(t *testing.T)` with
      `require.X(t, …)` / `assert.X(t, …)`.

   No test performs real network I/O — HTTP is faked with `jarcoal/httpmock`,
   and suites assert `httpmock.GetTotalCallCount()`. Test fixtures use fixed
   constants, never randomly generated values.

3. **Godoc conventions**: This is a published library — godoc is part of the
   deliverable, and brevity is part of the standard, not a trade against it.
   * Every package has a package comment on one file, opening
     `// Package <name> ...`, saying what the package is for in one or two
     sentences.
   * Every exported identifier has a doc comment beginning with its own name,
     in complete sentences. One or two lines; three only when the type
     genuinely needs it.
   * Comment the non-obvious: a decision, a constraint, a unit, a bug being
     prevented, an ordering that matters. Two lines is the working limit.
   * Do not comment what the code already says. No `TODO`, `FIXME`, or
     commented-out code — `godox` rejects them.

4. **Branch Naming**: Any branch created for feature work **MUST** use the
   prefix `feature/` (e.g. `feature/quote-cache`). Never `feat/`, `feat-`, or
   any other variant.

5. **Commit Messages**: Conventional Commits subjects. Never add
   `Co-Authored-By` and/or `Claude-Session` trailers — no AI/agent
   attribution trailers of any kind.

---

## Core Architectural Constraints

1. **CGO-Free SQLite (`CGO_ENABLED=0`)**: The binary must build with
   `CGO_ENABLED=0`. The SQLite driver is `modernc.org/sqlite`, never
   `mattn/go-sqlite3`.
2. **Logging**: `log/slog` only. Constructors take a `*slog.Logger`; there is
   no package-level logger (`sloglint` sets `no-global: all`). Log messages
   are lowercased; attribute keys are `snake_case`.
3. **API shape**: Domain methods return values, never pointers, and never
   `(nil, nil)`. Absence is `ErrNotFound`, wrapped with `%w` at each layer —
   a `//nolint:nilnil` is never the right fix. Errors crossing a package
   boundary are wrapped with `%w` and context. Use `slices.SortFunc`, never
   `sort.Slice`.
4. **Linting**: `.golangci.yml` is carried over from `tgshd` verbatim apart
   from the `gci`/`gofumpt` module paths. Do not disable a linter to make
   code pass. Every `//nolint` directive must be specific and carry an
   explanation. `forbidigo` forbids `fmt.Print*`: command output goes to an
   injected `io.Writer`, not stdout directly.

---

## Releases

This project deliberately has no release process: no `CHANGELOG.md`, no
`.cliff.toml`, no release workflow.

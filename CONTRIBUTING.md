# Contributing to LocalHarness

Thank you for your interest in contributing to **LocalHarness**! We welcome contributions, bug reports, feature requests, and documentation improvements.

---

## Development Prerequisites

- **Go**: Version 1.25 or higher
- **Make**: Standard build automation tool
- **Buf**: Optional, required only for regenerating protobuf definitions from `proto/`
- **Node.js**: Version 20+ (optional, only required if contributing to `gui/`)
- **Docker**: Optional, for container-based verification

---

## Local Development Workflow

### 1. Clone & Build

```bash
git clone https://github.com/divmora/localharness.git
cd localharness

# Compile local binary into bin/localharness
make build

# Build CLI debugger lhctl
make build-lhctl
```

### 2. Running Tests

```bash
# Run all unit tests
make test

# Run tests with race detector
go test -race -count=1 ./...
```

### 3. Code Formatting & Linting

```bash
# Format Go source files
make fmt

# Run static analysis and linting
make lint
```

---

## Available Make Targets

| Target | Description |
|---|---|
| `make build` | Compile local binary into `bin/localharness` |
| `make test` | Run test suite across all packages |
| `make fmt` | Format Go source code (`gofmt`) |
| `make lint` | Run protobuf linting (`buf lint`) and Go static analysis (`go vet`) |
| `make proto` | Regenerate Go protobuf code from `proto/localharness/v1/` |
| `make build-lhctl` | Compile `lhctl` CLI debugger into `bin/lhctl` |
| `make test-client` | Compile reference test client into `bin/testclient` |
| `make all` | Full build: proto + binary + testclient + lhctl |
| `make cross-compile` | Build release archives for linux/darwin amd64/arm64 |
| `make clean` | Clean build artifacts (`bin/` and `dist/`) |

---

## Conventional Commits

We adhere to the [Conventional Commits](https://www.conventionalcommits.org/) specification for automated release management via Release Please:

```
<type>(<scope>): <short summary>

[optional body]

[optional footer(s)]
```

### Common Types:
- `feat`: New user-facing feature or tool (triggers minor release bump).
- `fix`: Bug fix in runtime engine, tools, or transports (triggers patch release bump).
- `docs`: Documentation updates in `docs/` or `README.md`.
- `chore`: Dependency updates, build adjustments, or internal maintenance.
- `test`: Adding or updating test suites.
- `ci`: Changes to GitHub Actions workflows or GoReleaser.
- `refactor`: Internal code refactoring with no behavior change.

---

## Pull Request Guidelines

1. **Branch Naming**: Use descriptive branch names like `feat/mcp-resources` or `fix/pipe-handshake-timeout`.
2. **Keep Docs in Sync**: When modifying tool schemas, CLI flags, or protocol fields, update the corresponding documentation in `docs/`, `README.md`, and `examples/`.
3. **No Generated Files**: Do not manually edit `gen/go/` files; modify `proto/localharness/v1/localharness.proto` and run `make proto`.
4. **Clean Verification**: Ensure `make fmt`, `make lint`, and `make test` pass with zero errors before submitting.

---

## Licensing of Contributions

By submitting a pull request or contributing to this repository, you agree that your contributions will be licensed under the project's **Apache License, Version 2.0**, as detailed in [LICENSE](LICENSE).

# Contributing to Moca

Thank you for your interest in contributing to the Moca framework!

## Getting Started

1. Fork the repository
2. Clone your fork and set up the development environment — see [Development Setup](https://github.com/osama1998H/Moca/wiki/Contributing-Development-Setup)
3. Create a feature branch from `main`
4. Make your changes
5. Submit a pull request

## Development

- **Environment setup:** [Development Setup](https://github.com/osama1998H/Moca/wiki/Contributing-Development-Setup)
- **Code style:** [Code Conventions](https://github.com/osama1998H/Moca/wiki/Contributing-Code-Conventions)
- **Running tests:** [Testing Guide](https://github.com/osama1998H/Moca/wiki/Contributing-Testing-Guide)
- **CI pipeline:** [CI/CD Pipeline](https://github.com/osama1998H/Moca/wiki/Contributing-CI-CD-Pipeline)

## Quick Reference

```bash
make build              # Build all binaries
make test               # Run tests with race detector
make test-integration   # Run integration tests (requires Docker)
make lint               # Run golangci-lint
```

## Reporting Bugs

Open a [GitHub Issue](https://github.com/osama1998H/Moca/issues/new) with:
- Steps to reproduce
- Expected vs actual behavior
- Go version, OS, and Moca version (`moca version`)

## Proposing Features

Open a [GitHub Issue](https://github.com/osama1998H/Moca/issues/new) describing:
- The problem you're trying to solve
- Your proposed solution
- Any alternatives you've considered

## Pull Request Process

1. Ensure tests pass locally (`make test && make lint`)
2. Write tests for new functionality
3. Keep PRs focused — one feature or fix per PR
4. Update documentation if your change affects user-facing behavior

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).

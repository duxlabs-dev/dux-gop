# Contributing

## Setup

Enable the pre-commit hook:

```bash
git config core.hooksPath .githooks
```

Install golangci-lint:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Pre-commit checks

The pre-commit hook runs automatically on staged `.go` files:

1. `gofmt -s` - Format check
2. `golangci-lint` - Linting
3. `go test` - Tests
4. `go build` - Build verification

## Manual checks

```bash
# Format
gofmt -s -w .

# Lint
golangci-lint run ./...

# Test
make test

# Build
make
```

## Generative AI

Using an LLM to help write code is fine. Submitting its output unread is not.

- You are responsible for every line you submit. You have read it, tested it, and can explain it in review.
- Say in the PR when a meaningful part of the code was AI generated.
- Write PR descriptions, issues, and comments yourself. No AI-written or AI-rewritten text. A few plain sentences in your own words beat a page of generated summary. PRs and issues with AI-written text get closed without review.
- Do not file issues or leave comments you have not verified yourself. Reports that do not reproduce get closed.
- PRs that read like unreviewed output, with narrating comments, invented APIs, or style that ignores the file they are in, get closed without review.

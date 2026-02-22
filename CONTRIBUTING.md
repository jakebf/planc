# Contributing

Thanks for your interest in contributing to `planc`.

## Issues

- Use GitHub Issues for bugs, regressions, and feature ideas.
- Include your OS, Go version, and clear reproduction steps for bugs.
- If possible, include screenshots or terminal output.

## Pull Requests

- Keep changes focused and scoped to one improvement.
- Add or update tests when behavior changes.
- Update docs when user-facing behavior changes.

Before opening a PR, run:

```bash
go test ./...
go vet ./...
```

## Discussion

If you are unsure about an approach, open an issue first so we can align before implementation.

## Releases

Release process is documented in `RELEASING.md`.

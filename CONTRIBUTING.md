# Contributing to kprompt

Thanks for helping make **Talk to Your Cluster** better.

## Where to contribute

| Area | Repo |
|------|------|
| CLI + Observe agent (Go) | this repo — [`kprompt/kprompt`](https://github.com/kprompt/kprompt) |
| Kind demos | [`kprompt/kprompt-examples`](https://github.com/kprompt/kprompt-examples) |
| Website / install | [`kprompt/kprompt-website`](https://github.com/kprompt/kprompt-website) |
| Homebrew formula | [`kprompt/homebrew-tap`](https://github.com/kprompt/homebrew-tap) |

Product questions and demos: [Discussions](https://github.com/kprompt/kprompt/discussions). Architecture ADRs live in a private repo; public behavior changes belong in issues/PRs here.

## First contribution path

If this is your first open-source PR, start here:

1. Pick an issue labeled [`good first issue`](https://github.com/kprompt/kprompt/labels/good%20first%20issue) (or [`help wanted`](https://github.com/kprompt/kprompt/labels/help%20wanted)).
2. Comment on the issue: `I'd like to take this` so we can assign you.
3. Fork → branch → small focused PR linking the issue (`Fixes #N`).
4. Run the local checks below before pushing.

Newcomers are especially welcome on **docs**, **flag help text**, **Helm NOTES**, and **pure unit tests**. You do not need an LLM API key or a live cluster for most of those.

## Before you start

1. Search [issues](https://github.com/kprompt/kprompt/issues) and [discussions](https://github.com/kprompt/kprompt/discussions) for duplicates.
2. For non-trivial features, open an issue first.
3. Keep PRs focused — one concern per PR.

## Development (CLI)

```bash
git clone https://github.com/kprompt/kprompt.git
cd kprompt
go test ./...
go build -o bin/kprompt ./cmd/kprompt
./bin/kprompt version
./bin/kprompt doctor
```

Optional kind E2E:

```bash
go test -tags=e2e ./test/e2e/
```

Zero-key Observe demo (separate repo):

```bash
git clone https://github.com/kprompt/kprompt-examples.git
cd kprompt-examples && make walkthrough
```

## Pull requests

- Describe **why** the change matters, not only what changed
- Add / update tests when behavior changes
- Do not commit secrets, API keys, or kubeconfigs
- Match existing Go style (`gofmt`, package layout under `internal/`)
- Link related issues

## Security

Do not report vulnerabilities in public issues. See [SECURITY.md](./SECURITY.md) if present, or email the maintainers privately via the GitHub org.

## Code of Conduct

Be respectful. Assume good intent. We will close PRs/issues that include harassment or spam.

## License

Contributions are accepted under the [Apache License 2.0](./LICENSE).

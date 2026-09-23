# network-skill-lab — working agreements

## Three-tier development model

Production code is built by three roles, each on a specific model:

| Role | Model | Does | Does not |
|---|---|---|---|
| Planner | Claude Fable 5.1 (main session) | Breaks work into tasks, writes specs with acceptance criteria, sets coding standards, merges | Write production code |
| Developer | Claude Opus 5 (`Agent`, `model: "opus"`) | Implements one task from its spec, adds tests, reports what was verified | Change scope or decisions; if the spec is unclear, stop and ask the planner |
| Reviewer | Claude Haiku 4.5 (`Agent`, `model: "haiku"`) | Reviews the diff against the spec and standards, reports findings ranked by severity | Rewrite code |

Flow: planner writes one spec per phase -> user approves it -> planner splits it into tasks -> developer implements each task on a feature branch -> reviewer reviews -> planner resolves findings (may loop developer) -> PR to `main`.

Specs are one HTML file per phase in `docs/specs/phase<N>.html`, written in Traditional Chinese, and are also published as an artifact for the user to read. No phase starts until the user approves its spec. Task splits and acceptance criteria come from the approved spec; the planner does not re-open approved decisions.

**Exception — spikes.** Code under `spikes/` is throwaway verification work. The planner may write and run it directly without the developer/reviewer tiers. Spike results are written back to `docs/`.

## Rules

- **Design before code.** `docs/DESIGN.md` is the source of truth. Decisions D1..Dn are settled; changing one needs explicit user confirmation.
- **Architecture over content.** Labs, docs and tracks are fixtures that exercise the engine. The goal is a content system that accepts new items without code changes, not the number of items shipped.
- **Spikes never ship.** Production code must not import from `spikes/`.
- **Engine tests never depend on shipped content.** Tests outside `internal/content` use frozen fixtures under `internal/content/testdata/` (loaded via a helper), never `content/`. `content/` is exercised only by the content loader test and `nsl content lint`, so labs can evolve without breaking the engine suite.
- **Branching.** GitHub flow: feature branch -> PR -> squash merge into `main`. Never commit directly to `main`.
- **Naming.** Go module `github.com/AmerDwight/network-skill-lab`, binary `nsl`, image `nsl/node`, env prefix `NSL_`.
- **Code style.** Standard idiomatic style only: `gofmt` + `golangci-lint` defaults for Go, `eslint` + `prettier` defaults with strict TypeScript. No project-specific conventions beyond that.
- **Comments.** Write none unless the code cannot explain itself (a non-obvious invariant, a workaround with a reason). No comments that restate the code, no section banners, no commented-out code.
- **Language.** Talk to the user in Traditional Chinese. Code, identifiers and commit messages in English. `docs/DESIGN.md` and specs in Traditional Chinese.

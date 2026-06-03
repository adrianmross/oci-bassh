# Changelog

## Unreleased

- Let `ensure` and `ensure-target` accept the advertised root `--wait-timeout`
  flag before or after the subcommand and pass it through to
  `bastion-session ensure`.
- Add `hop setup shell` to print or install zsh integration with completions and
  the optional `hssh <host>` helper.

## v0.6.5

- Add visible `hop <host>` progress while auth and Bastion session renewal are running.
- Add `--wait-timeout` for the root host action and pass it through to `bastion-session ensure`.
- Keep JSON mode quiet by writing progress only for text output.

## v0.6.4

- Document required dependencies before install.
- Add maintainer guidance explaining why release automation does not update this file.

## v0.6.3

- Remove the `oci-bassh` compatibility binary from releases, source install, and Homebrew packaging.
- Simplify the README around install, quickstart, and help.
- Move automation and output-contract notes to `AGENTS.md`.

## v0.6.2

- Update the README terminal demo to show `hop explain`, `hop`, and then explicit `ssh my-vps-01`.

## v0.6.1

- Update source install, upgrade, plugin metadata, and Go module paths to the canonical `adrianmross/oci-hop` repository.

## v0.6.0

- Rename the product to OCI Bastion Hopper with `hop` and `oci-hop` primary commands.
- Add `hop <host>` as the safe default host action: prepare the route and stop before SSH.
- Keep `oci-bassh` as a compatibility binary for existing scripts.
- Refresh README, agent docs, schemas, installer, GoReleaser config, and VHS demo capture.

## v0.5.9

- Simplify README agent-support wording.

## v0.5.8

- Describe portable agent packaging across skills, generic agents, and Codex plugin metadata.

## v0.5.7

- Replace the Python test harness with Go tests.

## v0.5.6

- Move demo-generation notes out of the README and into `AGENTS.md`.

## v0.5.5

- Colorize terminal demo output.

## v0.5.4

- Use `my-vps-01` as the fictional example host.

## v0.5.3

- Replace real host, user, and proxy examples with fictional values.

## v0.5.2

- Add a VHS-generated terminal demo GIF.

## v0.5.1

- Refresh the README with the demo and updated usage flow.

## v0.5.0

- Migrate the CLI dispatcher to Cobra while preserving JSON command contracts.
- Add `explain`, `paths`, dry-run-first `upgrade`, JSON version output, and richer generated shell completions.

## v0.4.0

- Add tolerant `doctor` diagnostics and strict `check` health gates.
- Add `inspect` for cached/no-live host inspection and `repair` for fix flows.
- Add CLI version aggregation, shell completions, and `track --terraform-dir`.
- Extend JSON contracts and E2E coverage for diagnostics, repair, and import UX.

## v0.3.4

- Seed current Bastion state in real E2E tests.

## v0.3.3

- Clean Python cache artifacts before release packaging.

## v0.3.2

- Dispatch the release workflow after auto-tagging.

## v0.3.1

- Pass the release tag explicitly to GoReleaser.

## v0.3.0

- Promote `oci-bassh` to a releaseable Go CLI.
- Add GoReleaser, installer, and release workflows.
- Add JSON contract schema files for CLI outputs.
- Update E2E to exercise the Go CLI against fake and real downstream CLIs.

## v0.2.0

- Add the `bin/oci-bassh` command wrapper.
- Add short helper aliases: `track`, `ensure`, and `ssh`.
- Extend E2E coverage for command aliases and SSH dry-run output.

## v0.1.0

- Add the `oci-bassh` Codex plugin manifest.
- Add `oci-context` and `bastion-session` skills for agent-safe OCI Bastion SSH workflows.
- Add deterministic helper commands for doctor, target tracking, ensure, and contract checks.
- Add fake-CLI and real-binary E2E validation.

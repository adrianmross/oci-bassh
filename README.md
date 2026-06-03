# OCI Bastion Hopper

Prepare SSH access to OCI compute hosts through OCI Bastion without making the
operator think about sessions, OCIDs, or temporary bastion hostnames.

![OCI Bastion Hopper terminal demo](docs/assets/oci-hop-demo.gif)

`hop` is the small front-door CLI for the OCI SSH workflow:

- prepare `ssh <host>` without opening a shell session
- explain why a host-facing SSH route will or will not work
- track a compute instance from Terraform outputs
- create or renew the OCI Bastion managed SSH session
- write the VM-facing SSH config so the final command is still `ssh <host>`

It wraps the lower-level `oci-context` and `bastion-session` CLIs with one
operator-friendly Cobra command surface.

## Required Dependencies

- `oci-context`
- `bastion-session`
- OCI CLI (`oci`) with a working config/auth profile and access to the target tenancy and Bastion resources.

## Install

Homebrew is the preferred install path and installs `hop` and `oci-hop` under
Homebrew's bin directory, such as `/opt/homebrew/bin` on Apple Silicon macOS:

```bash
brew tap adrianmross/tap
brew install oci-hop
```

Source install is also supported. It installs to `/usr/local/bin` by default;
set `PREFIX` to choose another install prefix.

```bash
curl -sSL https://raw.githubusercontent.com/adrianmross/oci-hop/main/install.sh | bash
```

## Quickstart

Check local dependencies and context:

```bash
hop doctor
```

Explain the host-facing SSH path:

```bash
hop explain my-vps-01
```

Create or reuse the bastion session, update SSH config, and stop before SSH:

```bash
hop my-vps-01
```

Install shell integration for completions and the optional `hssh` helper:

```bash
hop setup shell --install
```

The primary workflow remains `hop <host>` followed by `ssh <host>`. The `hssh`
helper is for people who want one command that prepares the route and then
connects.

Use a longer session wait when OCI Bastion takes more time to activate:

```bash
oci-hop --wait-timeout 3m ensure my-vps-01
oci-hop ensure my-vps-01 --wait-timeout 3m
```

Successful preparation prints a compact status line:

```text
ready  my-vps-01  10.0.1.25  via my-bastion
```

Connect to the compute instance, not to the bastion alias:

```bash
ssh my-vps-01
```

If you want `oci-hop` to prepare the route and then run the SSH command, use:

```bash
oci-hop ssh my-vps-01
```

## Host Model

The durable target is the compute host alias you already type, such as
`my-vps-01`.

`hop` keeps the internal bastion jump host fresh, but the operator-facing
target remains:

```sshconfig
Host my-vps-01
  HostName 10.0.1.25
  User cloud-user
  ProxyJump my-bastion
```

That means `ssh my-vps-01` goes through OCI Bastion while still landing directly
on the compute instance.

## Help

Use Cobra help for the full command reference:

```bash
hop --help
hop <command> --help
```

The qualified command is available when scripts need a less generic binary
name:

```bash
oci-hop my-vps-01
```

## Agent Support

Reusable agent guidance lives in:

- `skills/`: runtime-neutral workflow instructions for `oci-context` and
  `bastion-session`
- `agents/`: adapter metadata and quick prompts for different agent runtimes
- `.codex-plugin/`: Codex packaging for the same portable skills

## Development

```bash
go test ./...
go vet ./...
go build ./cmd/oci-hop
go test -tags=e2e ./...
```

The default Go test suite includes strict hermetic command-contract coverage.
The `e2e` tagged tests use fake `oci` and `ssh` shims while exercising real
`oci-context` and `bastion-session` binaries. Override those binaries with:

```bash
OCI_CONTEXT_BIN=/path/to/oci-context \
BASTION_SESSION_BIN=/path/to/bastion-session \
go test -tags=e2e ./...
```

JSON schema files live under `schemas/`. The Go tests validate top-level
compatibility for the public command contract.

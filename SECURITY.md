# Security Policy

XClaw drives coding agents with broad tool access and connects to IM networks,
so it sits in a security-sensitive position. We take reports seriously.

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's [private vulnerability reporting][gh-report]
("Report a vulnerability" on the repository's **Security** tab). Include:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- affected component (`core`, `app`, `proto`) and version/commit.

We aim to acknowledge reports within a few days and will keep you updated on
remediation. Please give us reasonable time to fix the issue before any public
disclosure.

[gh-report]: https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability

## Supported versions

XClaw is pre-1.0 and under active development. Only the latest `main` receives
security fixes until a tagged release line exists.

## Threat model & known limitations

XClaw's design defends against prompt injection from untrusted group chat
(`core/safety/`, `core/groupctx/`). However, the project is **pre-1.0 and not yet
hardened for hostile multi-tenant deployment.** Be aware of these *known,
documented* limitations before exposing a bot to untrusted users:

- **Agent runs with full tool access.** The Claude driver always spawns with
  `--permission-mode bypassPermissions` (a headless invariant — there is no
  terminal to answer approval prompts). The agent can run arbitrary tools,
  including Bash.
- **The session sandbox is not a security boundary.** `core/sandbox/` gives each
  session a *starting* working directory, not a chroot/jail. An agent with Bash
  can still reach absolute paths outside it. Isolation across spaces relies on
  running one bot per space with a separate `cwdBase`.
- **Token storage.** The macOS app stores bot tokens in the **Keychain** and
  injects them into the daemon at runtime over the control bus (`secret.inject`);
  the core holds them in memory only and never writes them to disk. Tokens may
  still be placed **in plaintext** in `~/.xclaw/config.json` for headless/no-GUI
  deployments — protect that file accordingly. Tokens are also passed into the
  agent subprocess environment at turn time.
- **IM wire crypto is constrained by protocol compatibility.** The Octo/WuKongIM
  connector (`core/im/octo/`) reproduces the upstream handshake byte-for-byte
  (curve25519 DH → MD5-derived AES-128-CBC, IV from the server salt). These
  primitives are weak by modern standards but are dictated by wire compatibility
  with the server, not a free design choice; the connector only *decrypts*
  server-sent frames.
- **SSRF validation is config-time.** `core/config/` validates configured URLs,
  but does not currently defend against DNS rebinding at request time.

If you find a way to defeat the prompt-injection defenses, escape the sandbox in
a way the docs claim is prevented, or leak secrets through events/logs, that is
in scope — please report it.

## Hardening since v0.1

These changes have landed in the four post-v0.1 audit rounds. They are not
"new features" — they close concrete vectors operators should know about when
choosing what version to deploy:

- **Credentialed handshake host-pinning** (`core/im/octo/rest.go`). The
  server-returned `ws_url` is now validated against the operator-configured
  `apiUrl` before the connector dials: `wss://` required (or `ws://` only when
  `apiUrl` is itself loopback), and the WS hostname must equal the API hostname
  (case-insensitive). Without this, a compromised or MitM'd octo-server could
  return `ws://attacker/` and the WS dialer would accept plaintext + arbitrary
  host, leaking the bot's IMToken in the CONNECT frame.
- **Add-bot POST refuses redirects** (`desktop/internal/octoapi/octoapi.go`).
  The wizard's `POST /v1/user/bots` (which carries the operator's `uk_` User
  API Key as a bearer) now refuses any `3xx`. Go strips `Authorization` only
  on a cross-*host* redirect — a same-host or sibling-subdomain `302` would
  otherwise leak the key. `serverMsg` additionally strips control chars + caps
  length before the error reaches the UI toast.
- **Octo-cli download SSRF guard** (`desktop/internal/octocli/octocli.go`).
  Replaces `http.DefaultClient` with a dialer that rejects connections to
  private/loopback/link-local/CGN ranges. GitHub asset URLs redirect through
  S3/Fastly; a poisoned DNS or compromised mirror could otherwise redirect to
  `169.254.169.254` (cloud metadata) or a private internal address.
- **Wire-protocol DoS sentinels** (`core/im/octo/wire.go`). The frame parser
  now exports `ErrUnknownPacketType`, `ErrVarintTooLong`, `ErrFrameBodyTooLarge`,
  `ErrSocketClosed`, and the PKCS7 padding sentinels so downstream operators
  can `errors.Is`-match on the failure mode for metrics/alerting. The 8 MiB
  body cap + 4-byte varint cap are unchanged; only the error API was firmed up.
- **Token redaction on child-process output**
  (`desktop/internal/octocli/octocli.go`). Any `bf_*` / `uk_*` / `sk_*` /
  `sk-*` / `ANTHROPIC_*` substring in error output bubbled up from `octo-cli`
  is replaced with `<redacted>` before logs / UI. Octo-cli doesn't currently
  echo tokens — this is defense-in-depth against a future regression.
- **First-write atomicity for SOUL.md/AGENTS.md**
  (`desktop/internal/configstore/configstore.go`). Template scaffolding now
  uses `O_CREATE|O_EXCL|O_WRONLY`. The prior Stat-then-write derivation of
  "first time" was a TOCTOU: an agent that planted `SOUL.md` between our Stat
  and our write would have been silently overwritten. Blanking the field on
  an existing bot is now a NO-OP rather than a silent delete.
- **Slug validation in the secrets package**
  (`desktop/internal/secrets/secrets.go`). `Set`/`Get`/`Delete` now reject any
  `botID` that fails `safepath.ValidSlug`. The slug rule also rejects leading
  `.` so a bot id can't collide with dotfiles under `~/.xclaw/`. Without this
  fence, a future caller passing an attacker-supplied id like `"../other"`
  would have written/read another bot's credential namespace.
- **OCTO_BOT_ID uniqueness enforced**
  (`desktop/internal/configstore/configstore.go`). Two bots sharing an
  `OCTO_BOT_ID` would share an `octo-cli` disk profile; deleting one would
  silently break the other's auth on its next agent spawn. Save now rejects
  the duplicate at write time.
- **Atomic config/cron writes with fsync** (`writeAtomic` in both
  `desktop/internal/configstore` and `core/cron`). `config.json` and
  `cron.json` are written via `O_CREATE|O_EXCL` + `Sync` + `Rename`, and the
  `.tmp` is removed on any failure between write and rename — so a power loss
  or process crash mid-write leaves either the old file or a fully committed
  new file, never a half-written one.
- **In-flight turn shutdown barrier** (`core/im/octo/connector.go` +
  `core/cmd/xclawd/*.go`). The daemon now waits for every `drainTurns` and
  `session.send` goroutine to finish before closing the store on SIGTERM.
  Previously the deferred `st.Close()` could fire while a turn was still
  mid-flush, producing `"database is closed"` errors that broke resume
  continuity and lost usage accounting silently.
- **Disk perm tightening**. Per-bot skills/workflows files are now `0o600`
  (they are executable code the agent CLI loads on next spawn); octo-cli
  download `.tmp` + `.prev` rollback are `0o700` (the prior `0o755` was
  world-executable during the brief window between write and rename).
- **Reproducible builds**. `xclawd` is cross-compiled with `-trimpath
  -buildvcs=false` so binaries don't embed the operator's `$HOME` /
  module-cache absolute paths or the local VCS-dirty flag.
- **CI coverage**. `govulncheck` runs on both the `core` and `desktop`
  modules now (was core-only); `go test -race` runs on both Linux and macOS.

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
- **Atomic config/cron writes with fsync** (`core/atomicfile.Write`, called by
  both `desktop/internal/configstore` and `core/cron`). `config.json` and
  `cron.json` are written via `O_CREATE|O_TRUNC` + `Sync` + `Rename`, and the
  `.tmp` is removed on any failure between write and rename — so a power loss
  or process crash mid-write leaves either the old file or a fully committed
  new file, never a half-written one. **Limitation**: parent-directory `fsync`
  is omitted (industry-typical for application-level atomic writes); a power
  loss between rename and the next dirent flush could in principle resurrect
  the old file. SOUL.md / AGENTS.md *scaffolding* uses `O_CREATE|O_EXCL` so an
  agent-planted file is never overwritten on first save.
- **Cron task prompts are stored in plaintext** at `~/.xclaw/<id>/cron.json`
  (`0o600`, parent dir `0o755`). Operator-trusted content tier; same caveat as
  tokens-in-config above. If you grant `cron.create` to an authenticated peer,
  treat the resulting prompts as plaintext-at-rest.
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

## Trust model invariants

These invariants are load-bearing across the rounds 8–10 hardening work. They're
documented here so a future contributor doesn't accidentally weaken any of them.

### Cron-task ownership

- `cron.Manager.OwnerUID` is set from the **server-resolved** bot owner uid
  (from the octo `register` response), NEVER from a client-supplied body.
- `cron.create` is gated on the *server-resolved* owner uid — the body's
  `uid` field is ignored for authorization; it's only echoed into the task's
  `FromUID` for routing.
- On `SetOwnerUID(newUID)`, every persisted task whose `CreatedBy != newUID`
  is dropped. This covers two scenarios:
    1. In-process owner change (bf_ token rotation while the daemon is up).
    2. First owner resolve after a daemon restart against tasks that
       `cron.json` carried over from a prior owner.
  Without this, an operator-handoff or attacker-rotation would silently
  inherit every prior owner's scheduled prompts.
- Cron prompts are operator-trusted content (only the bot owner can author
  them). Stored in plaintext at `~/.xclaw/<id>/cron.json` (mode `0o600`).

### Persona / OBO clones

- The persona grantor uid is set ONCE at daemon startup from per-bot config
  (`onBehalfOf.uid`), NEVER mutated at runtime. No code path reloads it
  without a daemon restart.
- OBO v2 fields on inbound messages (`obo_origin_channel_id`,
  `obo_respond_as`, etc.) are honored ONLY when `m.FromUID == c.persona.UID`
  (the configured grantor is the one relaying). A forged OBO v2 message from
  any other uid is dropped — the trust comes from the FROM uid, not the
  body claim.
- Cron-fired turns on a persona-clone bot reply `on_behalf_of` the
  configured grantor (same identity as live replies). Trust derives from
  the owner-prune (above), not from `task.CreatedBy` (which is always the
  bot owner uid in production, not the grantor).

### Inbound→outbound target binding (round 8 F1-Arch + round 9 F1)

- The reply target for each turn travels with the turn on the per-session
  queue (`queuedTurn.tgt`). `drainTurns` is the SOLE writer of
  `c.targets[key]` and sets it just before `gw.Handle` runs.
- `onInbound` and `EnqueueCron` enqueue the turn (with target) and do not
  touch `c.targets[key]` directly. This prevents the wrong-recipient
  delivery race that existed when concurrent inbound + cron on the same
  sessionKey stomped a shared per-key target slot.

### Cron shutdown ordering (rounds 8 F2 → 9 F2 → 10 R10-F1)

Graceful shutdown sequence in `runBot`:

1. `cm.Stop()` — halts the scheduler loop and *waits for the loop
   goroutine to exit* (round 10: prevents a tick from spawning a fire
   after Stop returned).
2. `cm.Wait()` — drains any in-flight `safeFire` goroutines.
3. `connector.WaitTurns()` — sets `closed=true` (refuses any further
   enqueue) then drains every `drainTurns` worker.
4. `rtBot.target.turnsWG.Wait()` — drains any control-bus `session.send`
   goroutines.
5. Return → deferred `st.Close()` fires.

A tick or inbound that lands between steps 1 and 3 is refused at
`enqueueTurn` (sees `closed=true`). Without this ordering a late cron fire
would write to a closed store — `"database is closed"` data loss.

### Agent subprocess environment

- The agent subprocess inherits ONLY the variables on `core/agent.envAllowlist`
  (HOME / PATH / locale / proxy trio / SSL CA bundle pointers / `LC_*`).
  Operator env that might carry secrets — `AWS_*`, `GH_TOKEN`,
  `OPENAI_API_KEY`, `SSH_AUTH_SOCK`, etc. — is dropped. Specifically NOT
  inherited: `SHELL` (dropped round 9), `NODE_OPTIONS` (dropped round 10
  — it's an RCE pass-through via `--require=/tmp/evil.js`).
- Per-bot env (`agent.env` in config) flows through `extra` and is the
  supported channel for operator-set variables; operators authoring
  `agent.env` are accepting responsibility for whatever they put there.

### Workspace file preview (`desktop/internal/workspace`)

- Refuses to **list or read** a fixed set of credential-bearing dotfiles
  (`.netrc`, `.npmrc`, `.git-credentials`, `id_rsa*`, `.pgpass`, `.my.cnf`)
  even if the agent has copied them into its cwd.
- Refuses to **descend into** a fixed set of credential-bearing dotdirs
  (`.aws` / `.azure` / `.gcloud` / `.ssh` / `.gnupg` / `.docker` / `.kube`
  / `.helm` / `.cloudflared` / `.terraform.d` / `.cargo` / `.m2` / `.gradle`
  / `.snowsql` / `.databricks` / `.config` / `.continue` / `.kaggle`).
- Never follows symlinks (would let `.claude/skills/<bundle>/` escape into
  the global skill catalog).

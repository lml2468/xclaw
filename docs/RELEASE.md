# Releasing XClaw

This is the operator-facing checklist for cutting a signed, notarized release.
The CI does all the building/signing/notarization; you supply the secrets once
and then push a tag.

## One-time setup

You need three Apple things, all from one paid Apple Developer account
(`developer.apple.com`):

1. A **Developer ID Application** certificate (NOT "Apple Development" — that's
   for testing only and won't notarize). Generate it in
   `developer.apple.com → Certificates`, download the `.cer`, open it to import
   into your login Keychain, then in Keychain Access right-click the resulting
   private key → Export → `.p12` (set a password — you'll need it for the
   secret below).
2. An **App Store Connect API key** for notarization. Go to
   `appstoreconnect.apple.com → Users and Access → Integrations → App Store
   Connect API`, create a key with the **Developer** role, download the `.p8`
   (it's only downloadable ONCE — save it). Note the **Key ID** and the
   **Issuer UUID** shown on the page.
3. Your **signing identity string**, of the form
   `Developer ID Application: Your Name (TEAMID)`. Find it with:
   ```bash
   security find-identity -p codesigning -v
   ```

### Repo secrets

Set under repo `Settings → Secrets and variables → Actions`:

| Secret | Value |
|---|---|
| `APPLE_SIGN_IDENTITY` | the full identity string (`Developer ID Application: Name (TEAMID)`) |
| `APPLE_CERT_P12_B64`  | `base64 -i cert.p12 \| pbcopy` from the exported `.p12` |
| `APPLE_CERT_PWD`      | the password you set when exporting the `.p12` |
| `APPLE_NOTARY_KEY_B64`| `base64 -i AuthKey_XXXX.p8 \| pbcopy` from the App Store Connect key |
| `APPLE_NOTARY_KEY_ID` | the Key ID (e.g. `ABCD1234EF`) |
| `APPLE_NOTARY_ISSUER` | the Issuer UUID |

### Smoke-test locally first

Build + sign + notarize on your laptop with the same env vars before pushing a
tag. The CI does the same thing; passing locally is a good sanity check.

```bash
# Local notary via a stored keychain profile (one-time):
xcrun notarytool store-credentials xclaw-notary \
  --key /path/AuthKey_XXXX.p8 --key-id ABCD1234EF --issuer <uuid>

XCLAW_SIGN_IDENTITY="Developer ID Application: …" \
XCLAW_NOTARY_PROFILE=xclaw-notary \
XCLAW_UNIVERSAL=1 \
zsh scripts/package-desktop.sh

# Inspect the notarized .app
spctl --assess --type execute -vv desktop/bin/xclaw.app
codesign --verify --deep --strict --verbose=2 desktop/bin/xclaw.app
```

## Cutting a release

1. Make sure `main` is green on CI.
2. Pick the version (semver — `vMAJOR.MINOR.PATCH`).
3. Tag and push:
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```
4. Watch the workflow: `gh run watch` or
   `https://github.com/lml2468/xclaw/actions/workflows/release.yml`.
5. When it goes green, the release appears at
   `https://github.com/lml2468/xclaw/releases/tag/v1.0.0` with:
   - `XClaw-<ver>-macos-universal.zip` — signed + notarized .app, both archs
   - `xclawd-<ver>-{darwin-arm64,darwin-amd64,linux-amd64,linux-arm64,windows-amd64.exe}` — headless daemon binaries for non-Mac platforms
   - `checksums.txt` — SHA256 of every asset above
6. Hand-edit the release body if you want a human summary at the top
   (auto-generated notes are good enough most of the time).

## Troubleshooting

- **"User interaction is not allowed" during codesign** — the temp keychain
  wasn't unlocked or the partition list wasn't set. Both happen in the
  `Import Apple Developer ID cert` step; check the step's log.
- **`notarytool` returns `Invalid`** — download the log it points at and fix
  whichever helper failed. Most common cause: a helper (xclawd, octo-cli)
  wasn't signed with the hardened runtime; the script signs both inside-out.
- **"The provided entity includes an attribute with a value that has already
  been used"** when re-tagging the same version — Apple's notary service
  remembers the .zip digest. Bump the patch version and re-tag; don't try to
  re-use a tag.
- **Releases page is missing daemon binaries** — check `STAGE=...` step
  output. If the asset names changed (e.g. someone tweaked
  `package-desktop.sh`), the `cp` step will fail loudly.

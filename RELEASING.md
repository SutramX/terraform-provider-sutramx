# Releasing to the Terraform Registry

The provider is published as `sutramx/sutramx` at
<https://registry.terraform.io/providers/sutramx/sutramx>. The registry takes
the namespace (`sutramx`) from the GitHub organization and the name (`sutramx`)
from the repository name `terraform-provider-sutramx`.

How it works: you push a `vX.Y.Z` tag, and `.github/workflows/release.yml` runs
GoReleaser (`.goreleaser.yml`). GoReleaser creates a GitHub release with these
assets, which is the layout the registry requires:

| Asset | |
|---|---|
| `terraform-provider-sutramx_X.Y.Z_<os>_<arch>.zip` | one per platform (darwin, linux, windows, freebsd × amd64/arm64/386/arm, no darwin/386) |
| `terraform-provider-sutramx_X.Y.Z_manifest.json` | copy of `terraform-registry-manifest.json` (protocol 6.0) |
| `terraform-provider-sutramx_X.Y.Z_SHA256SUMS` | checksums of the zips and the manifest |
| `terraform-provider-sutramx_X.Y.Z_SHA256SUMS.sig` | detached GPG signature of `SHA256SUMS` (binary) |

The registry listens for GitHub release webhooks. After the provider has been
published once, every new tag shows up there within a few minutes.

## One-time setup (owner)

### 1. Put the repository under the SutramX organization, public

The registry only lists **public** repositories. They must be named
`terraform-provider-<name>`. The namespace comes from the owner, so the
repository must belong to the `SutramX` GitHub organization.

The current remote is `github.com/SutramX/terraform-provider-sutramx`.
Move the repository with either option:

- **Transfer:** on GitHub, open the repository, then *Settings → General →
  Danger Zone → Transfer ownership* and choose `SutramX`.
- **Create a new one:** create `SutramX/terraform-provider-sutramx` as a
  public repository and push `main` to it.

Then:

```bash
git remote set-url origin https://github.com/SutramX/terraform-provider-sutramx.git
```

The Go module path is already `github.com/sutramx/terraform-provider-sutramx`
(GitHub paths are case-insensitive).

Make sure GitHub Actions is enabled for the organization and repository
(*Settings → Actions → General → Allow all actions*, or allow `actions/*`,
`hashicorp/*`, `crazy-max/ghaction-import-gpg` and `goreleaser/goreleaser-action`).
If workflows fail with `startup_failure`, check the organization's Actions
billing and spending limit.

### 2. Create the signing key

The registry accepts **RSA** (or DSA) keys only, not ECC/ed25519.

```bash
gpg --full-generate-key
#   kind: (1) RSA and RSA
#   size: 4096
#   expiry: 0 (does not expire) or a date you will remember to renew
#   name: SutramX Terraform Provider
#   email: an address you control, e.g. admin@sutramx.com
#   passphrase: a strong one; you need it for the PASSPHRASE secret

gpg --list-secret-keys --keyid-format LONG     # note the key id after "rsa4096/"
gpg --armor --export-secret-keys <KEY_ID> > private.asc   # for the GitHub secret
gpg --armor --export <KEY_ID>                 > public.asc    # for the registry
```

Keep a backup of the private key and passphrase in your password manager.
**Every** release must be signed with a key the registry knows.

### 3. Add the GitHub Actions secrets

In `SutramX/terraform-provider-sutramx`, open *Settings → Secrets and variables
→ Actions → New repository secret* and add:

| Name | Value |
|---|---|
| `GPG_PRIVATE_KEY` | the whole content of `private.asc`, including the `-----BEGIN PGP PRIVATE KEY BLOCK-----` lines |
| `PASSPHRASE` | the key's passphrase |

Or use the GitHub CLI:

```bash
gh secret set GPG_PRIVATE_KEY --repo SutramX/terraform-provider-sutramx < private.asc
gh secret set PASSPHRASE      --repo SutramX/terraform-provider-sutramx   # prompts
```

Then delete `private.asc` from disk (`rm -P private.asc` on macOS).

### 4. Register the public key and the provider in the registry

1. Go to <https://registry.terraform.io> and choose **Sign in** → **Sign in with GitHub**.
   Authorize the *Terraform Registry* OAuth app and **grant it access to the
   SutramX organization**. If the button is missing, an org owner must approve
   the app under *GitHub → SutramX → Settings → Third-party access → OAuth app policy*.
2. Open *User Settings → Signing Keys* (<https://registry.terraform.io/settings/gpg-keys>),
   choose **New GPG Key**, select the **sutramx** namespace and paste `public.asc`.
3. Publish the first release (below) **before** the next step. The registry
   rejects a repository that has no valid release.
4. Choose **Publish → Provider**, select the `SutramX` organization and the
   `terraform-provider-sutramx` repository, accept the terms and choose
   **Publish**. The registry adds a webhook to the repository, which picks up
   later releases.

## Every release

1. Make sure `main` is green in CI (build, vet, gofmt, tests against the fake API).
2. If the schema or `examples/` changed, regenerate the docs and commit them:

   ```bash
   make docs        # tfplugindocs v0.25.0; needs a Terraform CLI on PATH or downloads one
   git diff --exit-code docs/ || git commit -am "docs: regenerate"
   ```

3. Move the `Unreleased` heading in `CHANGELOG.md` to the version and date, for
   example `## 0.1.0 (October 5, 2026)`, and commit.
4. (Optional) Do a local dry run. It needs GoReleaser v2:

   ```bash
   goreleaser check
   goreleaser release --snapshot --clean --skip=sign,publish
   ls dist/
   ```

5. Tag and push. Tags must be semantic versions with a leading `v`:

   ```bash
   git checkout main && git pull
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

6. Watch *Actions → Release*. It imports the key, builds every platform, signs
   `SHA256SUMS` and publishes the GitHub release. **Do not** leave the release
   as a draft and do not edit its assets. The registry ignores draft
   releases and rejects releases with changed assets.

Use a pre-release tag (`v0.2.0-beta.1`) for test builds. Terraform does not
select those unless a configuration asks for that exact version.

## Verify a release

```bash
V=0.1.0
gh release download v$V --repo SutramX/terraform-provider-sutramx --dir /tmp/tfp-$V
cd /tmp/tfp-$V
gpg --verify terraform-provider-sutramx_${V}_SHA256SUMS.sig terraform-provider-sutramx_${V}_SHA256SUMS
shasum -a 256 -c terraform-provider-sutramx_${V}_SHA256SUMS
```

`gpg --verify` must say **Good signature** from the key you registered.
`shasum -c` must report **OK** for every zip and the manifest.

In the registry:

- <https://registry.terraform.io/providers/sutramx/sutramx> lists the version,
  and its *Documentation* tab shows the index, 3 resources and 4 data sources
  from `docs/`.
- If a version is missing, check *Settings → Webhooks* in the GitHub repository
  for failed deliveries. You can also choose **Resync** on the provider's
  registry settings page.

End-to-end from a clean directory:

```bash
mkdir /tmp/tf-sutramx && cd /tmp/tf-sutramx
cat > main.tf <<'EOF'
terraform {
  required_providers {
    sutramx = { source = "sutramx/sutramx", version = "0.1.0" }
  }
}
provider "sutramx" {}
data "sutramx_regions" "all" {}
output "regions" { value = data.sutramx_regions.all.codes }
EOF
terraform init    # downloads from the registry and checks the signature
SUTRAMX_API_KEY=sk_... terraform apply   # data source only, creates nothing
```

## If something goes wrong

- **The registry says "signature invalid" or "key not found":** the key that
  signed the release is not the one registered for the `sutramx` namespace. Fix
  the secret or the registered key, delete the GitHub release **and** the tag,
  then tag again.
- **A bad release is out:** never re-use a version number. Publish a new patch
  version. On the registry's provider settings page you can delete a version
  so `terraform init` stops choosing it.
- **The key is compromised or expired:** create a new key, add it to the
  registry next to the old one, update both GitHub secrets, and remove the old
  key from the registry once the new release is listed.

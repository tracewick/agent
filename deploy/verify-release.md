# Release SBOMs and signatures

Releases made with the signed-release workflow provide:

- Two Linux archives (amd64/arm64), each accompanied by `<archive>.sbom.json` in
  CycloneDX JSON format. Syft scans the built archive, including the Go module,
  Go standard library version and binary hashes; it does not inventory the source tree.
- `checksums.txt`, covering the archives and their SBOMs, and its keyless Cosign
  bundle `checksums.txt.sigstore.json`. Authenticating this manifest and checking
  its hashes authenticates both kinds of files.
- A signed multi-platform GHCR image index. BuildKit's per-platform SPDX SBOM
  attestations are referenced by this index and include the base-image inventory.
  They are registry artifacts, separate from the archive SBOMs in GitHub Releases.

Existing 0.2.x releases remain unsigned. These instructions apply only after a
successful release using this workflow; changing the configuration does not sign
old downloads retroactively. An SBOM is an inventory, not a vulnerability verdict.
Scanners such as Trivy/Grype can consume it; the agent does not need it at runtime.

## Trust identity

Install Cosign 3 (the workflow uses 3.1.4). Require this exact certificate identity
and issuer, rather than accepting any valid Sigstore signature:

```bash
IDENTITY='https://github.com/tracewick/agent/.github/workflows/release.yml@refs/heads/main'
ISSUER='https://token.actions.githubusercontent.com'
```

The workflow runs on `main`, including manual recovery. Checking out a release tag
does **not** change its OIDC workflow identity to `refs/tags/...`. Fulcio issues a
short-lived certificate from GitHub's OIDC identity; no persistent signing key or
new signing secret is stored. Cosign records signatures in the transparency log.
The existing release App and GHCR credentials still serve their existing purposes.

## Verify an archive and its SBOM

Choose an actual signed release in GitHub Releases; replace the example version.
Use a new directory and download the four matching assets:

```bash
VER=1.0.0-rc.1   # example only; this does not imply that this release exists
ARCH=amd64      # or arm64
ARCHIVE="tracewick-agent_${VER}_linux_${ARCH}.tar.gz"
gh release download "v${VER}" --repo tracewick/agent \
  --pattern "$ARCHIVE" --pattern "$ARCHIVE.sbom.json" \
  --pattern checksums.txt --pattern checksums.txt.sigstore.json || exit 1

cosign verify-blob --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer "$ISSUER" \
  --bundle checksums.txt.sigstore.json checksums.txt || exit 1

# Run only after signature verification succeeds. Check each requested file,
# and fail if the signed manifest does not contain exactly one matching entry.
for FILE in "$ARCHIVE" "$ARCHIVE.sbom.json"; do
  awk -v name="$FILE" '$2 == name {print; count++} END {if (count != 1) exit 1}' \
    checksums.txt > selected-checksum.txt || exit 1
  sha256sum --check selected-checksum.txt || exit 1
done
```

Stop on any error, before extracting or executing the binary. A bare SHA-256
manifest provides integrity only; first authenticate it with Cosign. Each signed
manifest identifies its versioned archive filenames. Do not substitute the files
or manifest from another release. On macOS use `shasum -a 256 --check`.

## Verify a container and retrieve its SBOM

Resolve the selected version to its index digest, then verify and use that exact
digest throughout. Requires Docker Buildx for the inspection commands:

```bash
IMAGE=ghcr.io/tracewick/agent
DIGEST=$(docker buildx imagetools inspect "$IMAGE:$VER" --format '{{.Manifest.Digest}}') || exit 1
cosign verify --certificate-identity "$IDENTITY" \
  --certificate-oidc-issuer "$ISSUER" "$IMAGE@$DIGEST" || exit 1

# After successful verification, inspect/export the signed index's SBOMs.
docker buildx imagetools inspect "$IMAGE@$DIGEST" \
  --format '{{json .SBOM}}' > image-sboms.json
jq '.["linux/amd64"].SPDX' image-sboms.json > image-linux-amd64.spdx.json
docker pull "$IMAGE@$DIGEST"
```

The signature covers the **index digest**, which binds the platform manifests
and BuildKit attestations it references. It is not a separate Cosign SBOM
attestation; `cosign verify-attestation --type cyclonedx` is not its retrieval API.
An extracted SPDX JSON alone is not proof: keep its association with the verified
index. Pin the digest in deployment after verification; a tag may move.

## Maintainer workflow and recovery

release-please still creates the version/changelog/tag through the existing Git
flow. GoReleaser builds archives, Syft creates their SBOMs, Cosign signs the
checksum manifest, and Buildx publishes SBOM-bearing images which Cosign signs by
index digest. Only the publishing job receives `id-token: write`; PR checks do
not receive OIDC or publication permissions.

The release job checks out the actual tag and requires its commit to belong to
`main`. After publication it downloads the release assets and verifies the
manifest's keyless signature, hashes, both ELF architectures, release identity
and SBOM-to-binary binding. It also verifies the image index's signature,
version/revision and both platform SBOMs.
Any signing or verification failure fails the job; it does
not fall back to an unsigned release. GitHub Release or image publication may
already be partial when a later step fails. Treat only a completed workflow as a
completed release; do not announce a failed/partial publication as verified.

Manual recovery uses the existing `Release` workflow on **main** with
`publish_tag` set to the existing tag. The tagged source must contain this signed
release configuration and artifact verifier. Do not use it to retrofit old
unsigned tags. Rerunning rebuilds artifacts; it is not a promise of byte-identical
reproduction. Verify the successful rerun before using its resulting digests.

For local/PR validation (GoReleaser 2.20.2 and Syft 1.57.2):

```bash
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign,docker
python3 scripts/check-release-artifacts.py dist --unsigned \
  --version "$(jq -r .version dist/metadata.json)"
python3 -m unittest discover -s scripts -p 'test_*.py' -v
```

This checks real archive/SBOM output and negative controls without signing,
publishing or claiming an OIDC test. The first normal signed release must also
confirm GHCR signature/SBOM retrieval and asset verification with the production
identity. Snapshot success is not evidence that those remote steps have run.

References: [GoReleaser signing](https://goreleaser.com/customization/sign/sign/),
[archive SBOMs](https://goreleaser.com/customization/sbom/),
[Docker v2](https://goreleaser.com/customization/package/dockers_v2/).

#!/usr/bin/env python3
"""Verify downloaded release assets, or explicitly unsigned local snapshots."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile

IDENTITY = "https://github.com/tracewick/agent/.github/workflows/release.yml@refs/heads/main"
ISSUER = "https://token.actions.githubusercontent.com"


def check(directory, version, unsigned=False):
    manifest = directory / "checksums.txt"
    if not unsigned:
        subprocess.run([
            "cosign", "verify-blob", "--certificate-identity", IDENTITY,
            "--certificate-oidc-issuer", ISSUER,
            "--bundle", str(directory / "checksums.txt.sigstore.json"), str(manifest),
        ], check=True)

    entries = {}
    for line in manifest.read_text().splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9_.+-]+)", line)
        if not match or match[2] in entries or match[2] in (".", ".."):
            raise ValueError("Malformed or duplicate checksum entry")
        entries[match[2]] = match[1]
    if not entries:
        raise ValueError("Empty checksum manifest")
    for name, digest in entries.items():
        path = directory / name
        if path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError(f"Checksum mismatch: {name}")

    for arch, machine in (("amd64", 62), ("arm64", 183)):
        archive = f"tracewick-agent_{version}_linux_{arch}.tar.gz"
        sbom_name = archive + ".sbom.json"
        if archive not in entries or sbom_name not in entries:
            raise ValueError(f"Archive or SBOM missing from checksums: {arch}")
        with tarfile.open(directory / archive, "r:gz") as stream:
            member = stream.getmember("tracewick-agent")
            if not member.isfile():
                raise ValueError("Archive binary is not a regular file")
            binary = stream.extractfile(member).read()
        if binary[:6] != b"\x7fELF\x02\x01" or int.from_bytes(binary[18:20], "little") != machine:
            raise ValueError(f"Wrong ELF architecture: {arch}")

        sbom = json.loads((directory / sbom_name).read_text())
        component = sbom.get("metadata", {}).get("component", {})
        if (sbom.get("bomFormat") != "CycloneDX" or component.get("name") != "tracewick-agent"
                or component.get("version") != version):
            raise ValueError(f"Wrong SBOM format or release identity: {arch}")
        components = sbom.get("components", [])
        if not any(c.get("name") == "stdlib" and c.get("version") for c in components):
            raise ValueError(f"SBOM lacks the Go standard library: {arch}")
        if not any(c.get("name") == "github.com/tracewick/agent" and any(
                p.get("name") == "syft:metadata:architecture" and p.get("value") == arch
                for p in c.get("properties", [])) for c in components):
            raise ValueError(f"SBOM lacks the matching Go module architecture: {arch}")
        binary_hash = hashlib.sha256(binary).hexdigest()
        if not any(c.get("type") == "file" and c.get("name") == "/tracewick-agent" and any(
                h.get("alg") == "SHA-256" and h.get("content") == binary_hash
                for h in c.get("hashes", [])) for c in components):
            raise ValueError(f"SBOM does not describe this archive's binary: {arch}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--version", required=True, help="Version without the v prefix")
    parser.add_argument("--unsigned", action="store_true", help="Local/PR snapshots only; no identity claim")
    args = parser.parse_args()
    check(args.directory, args.version, args.unsigned)
    print("Verified archive hashes, architectures and SBOM contents" +
          (" (UNSIGNED snapshot)" if args.unsigned else " and release signature"))

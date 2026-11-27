# Offline release signatures

## Publisher key custody on KDE

`key_custody.py` is a separate, explicitly invoked Linux publisher tool. It uses
the system Python, `python3-cryptography`, GI/libsecret and GnuPG; these are not
product dependencies. It never invokes the signer, changes product trust roots,
or uploads anything. It refuses Secret Service providers other than KDE's
KWallet/ksecretd and only searches its own publisher attributes. The preflight
does not read existing secrets or create a key:

```sh
/usr/bin/python3 -s tools/signrelease/key_custody.py preflight
```

After reviewing the code and being ready to enter a backup passphrase locally:

```sh
/usr/bin/python3 -s tools/signrelease/key_custody.py create
```

GPG's local pinentry asks for the backup passphrase. Do not send it through chat,
arguments, environment variables or logs. The tool generates an Ed25519 seed in
memory, persists a GPG AES-256 encrypted recovery document **before** writing the
base64 seed into KWallet, then checks wallet readback and decrypts the backup to
confirm the same public key. A second pinentry prompt deliberately checks actual
recovery without relying on GPG's symmetric passphrase cache. No plaintext key
file is created. Core dumps are disabled, but Python/library memory erasure and
protection against malicious software in the same account are not guaranteed.
KWallet is secret storage, not a non-exportable hardware signing device.

New files under `~/.local/share/sidravia-signing/` are private (directory 0700,
files 0600). Existing entries/files, symlinks, shared directories and repository
paths are refused. The tool does not change wallet passwords, auto-unlock or
access policies. Only the newly created publisher entry is read back; it does not
export other secrets. It does not automatically retry after a partial failure.

- `publisher-ed25519-seed.gpg`: encrypted recovery document, suitable for manual
  upload to OneDrive. The cloud account only receives ciphertext.
- `publisher-public.txt`: base64 public key; not a secret.
- `publisher-key.json`: public identity, backup hash and successful custody checks.

Upload the encrypted `.gpg` backup yourself. Keep the backup passphrase in a
separate password manager. Download it again to a fresh path and verify it
against the independently retained public key:

```sh
/usr/bin/python3 -s tools/signrelease/key_custody.py verify-backup \
  --backup /absolute/path/to/downloaded-backup.gpg \
  --public-key "$HOME/.local/share/sidravia-signing/publisher-public.txt"
```

Verification decrypts only into process memory and does not access the wallet,
sign or upload. It requires the expected public key, so a different valid key's
backup is not accepted. Never upload a decrypted recovery document.

If an operation fails or is cancelled, preserve the wallet entry and any files
already created. An encryption cancellation leaves no key files or wallet entry;
a later failure can leave an encrypted backup/public key, and possibly the wallet
entry, without the final success record. A missing record is **not success**.
Do not delete them or generate another key: use `verify-backup` to establish which
identity remains, then explicitly arrange recovery of that same key. This tool
does not silently replace or delete a wallet entry. Formal signing, public-key
embedding and publication remain separate authorized operations.

## Signing an exact release manifest

This developer tool signs exact, reviewed v2 release metadata.
It does not generate or store private keys, rewrite the manifest, access the
network, upload a release or authorize installation. Run the fixed distribution
producer/consumer first: a signature approves release bytes; it is not a substitute
for source, package-content or native installation verification.

Use the pinned Go toolchain to run:

```
go run ./tools/signrelease --manifest /path/to/sidravia-release.json --output /path/to/sidravia-release.sig
```

Prepare one request per published metadata pair. The request's `expectedAssets`
lists the portable archives and Setup executables for that pair.

| Published manifests and signatures | Assets |
| --- | --- |
| `sidravia-release.json`, `sidravia-release.sig` | Existing gzip/ZIP portable and AMD64 Windows Setup, including ARM64 core portable |
| `sidravia-release-xz.json`, `sidravia-release-xz.sig` | Linux XZ assets for existing targets, with matching gzip ownership in the primary manifest |
| `sidravia-release-extended.json`, `sidravia-release-extended.sig` | ARM64 Linux GUI portable (gzip and/or XZ), ARM64 Windows GUI portable and ARM64 Windows Setup |

When publishing both encodings for the ARM64 Linux GUI, prepare each producer
output in its own directory and list both in the extended request. Both archives
must carry the same ownership index. Verify each metadata pair with
`dev.py release verify` against its own request. System-managed RPM and DEB
packages are discovered by package name and architecture, then installed by the
system package manager.

Standard input must contain a base64-encoded 32-byte Ed25519 seed supplied by the
local key-unlocking process. Keep the encrypted private key local; do not put the
seed in command arguments, environment variables, shell history or CI secrets.
The manifest key ID must already match the selected key. The tool bounds inputs,
verifies the complete manifest with the derived public key, and refuses an
existing output. The signature is raw 64-byte Ed25519 data, matching the updater's
fixed metadata reader. Key storage/unlocking and formal signing are separate
publisher operations; this implementation has only been tested with synthetic
in-memory keys. The product pins the publisher public key; private material stays
outside the repository and is not accessed by public verification or packaging.

The tool clears its owned seed buffers as a best effort; this is not a guarantee
that all copies inside the Go runtime or cryptographic implementation are erased.


## Public inspection and verification

These modes never read stdin or private keys, create a signature, execute a
product file, change product trust roots, or publish anything:

```sh
go run ./tools/signrelease inspect --manifest /path/to/sidravia-release.json --public-key /path/to/publisher-public.txt --json
go run ./tools/signrelease verify --manifest /path/to/sidravia-release.json --public-key /path/to/publisher-public.txt --signature /path/to/sidravia-release.sig --json
```

The public key file contains one base64-encoded 32-byte Ed25519 **public key**.
`inspect` validates the shared v2 structure and matching key ID; its result says
`signature: not_checked`. `verify` authenticates the exact manifest bytes and
reports `signature: passed`. For Qt releases, pass the directory of the signed
Core and GUI repository archives to verify the staged bytes and their embedded
publisher signatures against the release manifest.

```sh
go run ./tools/signrelease verify --published \
  --manifest /path/to/sidravia-release-qt.json --signature /path/to/sidravia-release-qt.sig \
  --public-key /path/to/publisher-public.txt --qt-artifacts /path/to/signed-repositories --json
```

The result reports `artifactBytes: passed` after checking all archives named in
the Qt manifest. Run native lifecycle acceptance separately with the matching
installer and test repositories.

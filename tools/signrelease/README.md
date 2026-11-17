# Offline release signatures

This developer tool signs the exact reviewed `sidravia-release.json` v2 bytes.
It does not generate or store private keys, rewrite the manifest, access the
network, upload a release or authorize installation. Run the fixed distribution
producer/consumer first: a signature approves release bytes; it is not a substitute
for source, package-content or native installation verification.

Use the pinned Go toolchain to run:

```
go run ./tools/signrelease --manifest /path/to/sidravia-release.json --output /path/to/sidravia-release.sig
```

Standard input must contain a base64-encoded 32-byte Ed25519 seed supplied by the
local key-unlocking process. Keep the encrypted private key local; do not put the
seed in command arguments, environment variables, shell history or CI secrets.
The manifest key ID must already match the selected key. The tool bounds inputs,
verifies the complete manifest with the derived public key, and refuses an
existing output. The signature is raw 64-byte Ed25519 data, matching the updater's
fixed metadata reader. Key storage/unlocking and formal signing are separate
publisher operations; this implementation has only been tested with synthetic
in-memory keys. No formal key is configured in the product.

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
reports `signature: passed`. Neither mode checks artifact bytes; the developer
release entry below performs that independent package consumption first. A
signature or public inspection alone is not native upgrade/channel acceptance.

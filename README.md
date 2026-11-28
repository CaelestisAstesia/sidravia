# Temporary Sidravia ARM64 CI input

This branch carries the x64-host Qt IFW toolkit used for an isolated ARM64
acceptance run. The toolkit contains a public fixture verification key. It is
for test use and has no Sidravia production trust or publishing authority.
The zip SHA-256 is
`d49a7389d25a20ee6c68ae7276a34b78f093566d318132b1da30c71d65255802`.

The corresponding source is supplied at the following locations. The matching
local integration identifies the tool versions, static library build options,
and upstream changes.

- [Sidravia IFW build and patches](https://github.com/CaelestisAstesia/sidravia/tree/163b5141dacf5e684755f19a291bed1a45f9b28a/packaging/windows/ifw)
- [Test job build recipe for the static libraries](https://github.com/CaelestisAstesia/sidravia/blob/aec75aeca51fc69fa106f237b60e975cd1bda9a5/.github/scripts/build-qt-arm64-test-kit.ps1)
- [Qt Installer Framework 4.11.0 source at the pinned commit](https://github.com/qtproject/installer-framework/tree/aac2cf9c93b80a0f5959ab6c8a00bc378c54fdec)
- [Qt 6.9.3 submodule sources](https://download.qt.io/archive/qt/6.9/6.9.3/submodules/) for QtBase, QtDeclarative, Qt5Compat, QtTools and QtTranslations.
- [libsodium 1.0.22 source](https://download.libsodium.org/libsodium/releases/libsodium-1.0.22.tar.gz)
- [XZ 5.8.1 source](https://github.com/tukaani-project/xz/releases/download/v5.8.1/xz-5.8.1.tar.xz)
- [bzip2 1.0.8 source](https://sourceware.org/pub/bzip2/bzip2-1.0.8.tar.gz)

See the accompanying upstream license files.

The current archive incorporates the shared legacy-installation guard and the refactored IFW integration. It retains the same disposable publisher and ARM64 payload binding.

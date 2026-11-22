final _releaseVersion = RegExp(r'^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$');

bool validReleaseVersion(String version) => _releaseVersion.hasMatch(version);

String? releasePageUrl(String version) => validReleaseVersion(version)
    ? 'https://github.com/CaelestisAstesia/sidravia/releases/tag/v$version'
    : null;

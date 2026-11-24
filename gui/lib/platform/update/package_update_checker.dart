enum UpdateCheckState {
  available,
  upToDate,
  aheadOfChannel,
  noCompatibleRelease,
}

typedef PackageUpdate = ({UpdateCheckState state, String? version});

abstract interface class PackageUpdateChecker {
  bool get supportsUpdateCheck;
  bool get usesSystemPackageManager;
  Future<PackageUpdate> checkUpdate();
  Future<void> openReleasePage(String version);
}

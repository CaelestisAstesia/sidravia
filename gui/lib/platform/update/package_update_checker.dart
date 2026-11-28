enum UpdateCheckState {
  available,
  upToDate,
  aheadOfChannel,
  noCompatibleRelease,
}

class PackageUpdate {
  const PackageUpdate({
    required this.state,
    this.version,
    this.notes = '',
    this.installCandidateId,
  });

  final UpdateCheckState state;
  final String? version;
  final String notes;
  final String? installCandidateId;
}

abstract interface class PackageUpdateChecker {
  bool get supportsUpdateCheck;
  bool get usesSystemPackageManager;
  Future<PackageUpdate> checkUpdate();
  Future<void> installUpdate(String candidateId);
  Future<void> openReleasePage(String version);
}

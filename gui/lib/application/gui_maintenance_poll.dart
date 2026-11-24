part of 'gui_controller.dart';

// The controller owns the timer and the connection generation.
extension _MaintenancePolling on GuiController {
  void _scheduleMaintenance(int generation, SidraviaIpcClient client) {
    if (client is! SidraviaMaintenanceClient ||
        _maintenanceTimer != null ||
        !_canContinue(generation)) {
      return;
    }
    _maintenanceTimer = Timer(const Duration(seconds: 1), () {
      _maintenanceTimer = null;
      if (!_canUseClient(generation, client)) return;
      if (_transition != null || _userBusy) {
        _scheduleMaintenance(generation, client);
        return;
      }
      _transition = _pollMaintenance(generation, client);
    });
  }

  Future<void> _pollMaintenance(
    int generation,
    SidraviaIpcClient client,
  ) async {
    var commit = false;
    try {
      final maintenance = client as SidraviaMaintenanceClient;
      final view = await maintenance.updateStatus();
      if (!_canUseClient(generation, client)) return;
      _maintenanceFrozen = view.phase != 'idle';
      _notify();
      if (view.phase == 'idle') {
        _maintenanceOperation = null;
        _maintenanceCommitDispatched = null;
      }
      if (view.phase == 'requested' &&
          _maintenanceOperation != view.operationId) {
        _maintenanceOperation = view.operationId;
        final consent =
            await onMaintenancePrepare?.call(view.interactive) ??
            (accepted: false, visible: false);
        if (!_canUseClient(generation, client)) return;
        await maintenance.updateAcknowledge(
          view.operationId!,
          consent.accepted,
          consent.visible,
        );
      }
      commit =
          view.phase == 'committed' &&
          _maintenanceOperation == view.operationId;
    } on Object catch (error) {
      if (_ownsClient(generation, client)) {
        await _invalidate(generation, client, error);
      }
    } finally {
      if (_generation == generation) _transition = null;
      _scheduleMaintenance(generation, client);
    }
    if (commit &&
        _canContinue(generation) &&
        _maintenanceCommitDispatched != _maintenanceOperation) {
      _maintenanceCommitDispatched = _maintenanceOperation;
      await onMaintenanceCommit?.call();
    }
  }
}

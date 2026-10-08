import 'package:sidravia_gui/ipc/state_models.dart';

/// One connection epoch. Closing or canceling events awaits the unsubscribe ACK.
abstract interface class StateSubscription {
  StateBootstrap get initial;
  Stream<StateEvent> get events;
  Future<void> close();
}

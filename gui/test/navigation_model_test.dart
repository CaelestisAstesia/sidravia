import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/app/app_destination.dart';

void main() {
  test('destinations have a stable unique order', () {
    expect(appDestinations.map((destination) => destination.id), [
      'home',
      'configuration',
      'settings',
    ]);
    expect(
      appDestinations.map((destination) => destination.id).toSet(),
      hasLength(appDestinations.length),
    );
    expect(
      appDestinations.map((destination) => destination.section).toSet(),
      hasLength(appDestinations.length),
    );
  });
}

import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> displayedSnapshot(WidgetTester tester) => jsonDecode(
  tester
      .widget<SelectableText>(
        find.byKey(const ValueKey('runtime-snapshot-text')),
      )
      .data!,
) as Map<String, dynamic>;

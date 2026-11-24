import 'dart:convert';
import 'dart:io';

Map<String, dynamic> readIpcConformanceFixture() => jsonDecode(
  File('../internal/ipc/contract/testdata/v1/conformance.json')
      .readAsStringSync(),
) as Map<String, dynamic>;

Map<String, dynamic> readIpcMethodFixture(String method) =>
    (readIpcConformanceFixture()['cases'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .singleWhere((entry) => entry['method'] == method);

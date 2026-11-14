import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/business_controller.dart';
import 'package:sidravia_gui/features/home/business_panel.dart';
import 'package:sidravia_gui/ipc/sidravia_ipc_client.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';

const vector =
    '{"sessionId":"s","runGeneration":9007199254740993,"revision":18446744073709551615,"current":true,"statisticsSupported":true,"textSupported":true,"statistics":{"receivedAt":"2026-11-14T01:02:03Z","source":"server_login","scope":"server_billing_history","trafficBytes":0,"balanceCents":null},"messages":[{"id":"1","receivedAt":"2026-11-14T01:02:03Z","text":"Hello"}],"updatedAt":"2026-11-14T01:02:03Z"}';
BusinessSnapshot sample({String id = 's', int gen = 1, int revision = 2}) =>
    decodeBusiness(
      vector
          .replaceFirst('"s"', jsonEncode(id))
          .replaceFirst('9007199254740993', '$gen')
          .replaceFirst('18446744073709551615', '$revision'),
    );
SessionSummary session({String id = 's', int gen = 1}) => SessionSummary(
  id: id,
  displayName: '',
  accountName: 'test',
  state: SessionState.authenticated,
  intent: SessionIntent.maintainAuthentication,
  protocolSocket: NetworkProtocolSocket(
    state: 'open',
    runGeneration: BigInt.from(gen),
  ),
);

class Query implements BusinessQuery {
  final done = Completer<BusinessSnapshot>();
  int cancels = 0;
  @override
  Future<BusinessSnapshot> get result => done.future;
  @override
  Future<void> cancel() async {
    cancels++;
  }
}

class BusinessClient implements SidraviaBusinessClient {
  final queries = <Query>[];
  @override
  BusinessQuery queryBusiness(String id) {
    final q = Query();
    queries.add(q);
    return q;
  }
}

void main() {
  test('independent nullable strict business contract', () {
    final v = decodeBusiness(vector);
    expect(v.revision, BigInt.parse('18446744073709551615'));
    expect(v.runGeneration, BigInt.parse('9007199254740993'));
    expect(v.statistics!.trafficBytes, 0);
    expect(v.statistics!.balanceCents, isNull);
    for (final bad in [
      vector.replaceFirst('"current":true', '"current":null'),
      vector.replaceFirst('"Hello"', r'"\u001bhello"'),
      vector.replaceFirst('"Hello"', r'"\ud800"'),
      vector.replaceFirst('"balanceCents":null', '"balanceCents":2147483648'),
      vector.replaceFirst(
        '"scope":"server_billing_history"',
        '"scope":"monthly"',
      ),
      vector.replaceFirst('"textSupported":true', '"textSupported":false'),
      vector.replaceFirst('"messages":[', '"messages":null,"extra":['),
    ]) {
      expect(() => decodeBusiness(bad), throwsA(isA<IpcProtocolException>()));
    }
  });
  testWidgets(
    'generation invalidates old response and visibility cancels one query',
    (tester) async {
      final c = BusinessController(pollDelay: const Duration(seconds: 5));
      final client = BusinessClient();
      void update(SessionSummary s, bool visible) => c.update(
        connection: client,
        client: client,
        session: s,
        visible: visible,
      );
      update(session(), true);
      expect(client.queries, hasLength(1));
      update(session(gen: 2), true);
      expect(client.queries.first.cancels, 1);
      expect(client.queries, hasLength(1));
      client.queries.first.done.complete(sample());
      await tester.pump();
      expect(c.snapshot, isNull);
      expect(client.queries, hasLength(2));
      client.queries.last.done.complete(sample(gen: 2));
      await tester.pump();
      expect(c.snapshot!.runGeneration, BigInt.two);
      await tester.pump(const Duration(seconds: 5));
      expect(client.queries, hasLength(3));
      update(session(gen: 2), false);
      expect(client.queries.last.cancels, 1);
      client.queries.last.done.complete(sample(gen: 2, revision: 1));
      await tester.pump();
      expect(c.snapshot!.revision, BigInt.two);
      await tester.pump(const Duration(seconds: 20));
      expect(client.queries, hasLength(3));
      update(session(gen: 2), true);
      expect(client.queries, hasLength(4));
      client.queries.last.done.completeError(
        const IpcRequestFailure('unknown_method'),
      );
      await tester.pump();
      expect(c.unsupported, isTrue);
      expect(c.snapshot, isNull);
      await tester.pump(const Duration(seconds: 20));
      expect(client.queries, hasLength(4));
      await c.close();
      c.dispose();
    },
  );
  testWidgets('plain text panel keeps zero separate from absent balance', (
    tester,
  ) async {
    final c = BusinessController();
    final client = BusinessClient();
    c.update(
      connection: client,
      client: client,
      session: session(),
      visible: true,
    );
    client.queries.single.done.complete(sample());
    await tester.pump();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: BusinessPanel(controller: c)),
      ),
    );
    expect(find.text('余额：暂无数据'), findsOneWidget);
    expect(find.text('服务器历史计费流量：0.00 MiB'), findsOneWidget);
    expect(find.text('Hello'), findsOneWidget);
    expect(find.byType(SelectableText), findsOneWidget);
    c.dispose();
  });
}

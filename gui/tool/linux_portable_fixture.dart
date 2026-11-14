// Owned native acceptance driver. Uses the production IPC and preferences
// contracts without changing the desktop/headless lifecycle boundary.
import 'dart:convert';
import 'dart:io';

import 'package:sidravia_gui/bootstrap/gui_bootstrap.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/ipc/web_socket_ipc_client.dart';
import 'package:sidravia_gui/platform/gui_settings_storage_io.dart';
import 'package:sidravia_gui/platform/linux_installation_manager.dart';
import 'package:sidravia_gui/platform/installation_manager.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

Future<void> main(List<String> args) async {
  final owner = Platform.environment['SIDRAVIA_GUI_FIXTURE'];
  final ns = Platform.environment['SIDRAVIA_NAMESPACE'];
  if (owner == null ||
      ns == null ||
      !ns.startsWith('linux-gui-test-') ||
      File('$owner/fixture-owner').readAsStringSync() != ns ||
      args.length != 2) {
    throw StateError('owned fixture required');
  }
  final runtime = File(args[1]);
  if (!runtime.absolute.path.startsWith('$owner/')) {
    throw StateError('outside fixture');
  }
  if (args[0] == 'autostart-enable' || args[0] == 'autostart-disable') {
    final executable = '${runtime.parent.parent.parent.path}/sidravia_gui';
    final manager = LinuxInstallationManager(
      executable: executable,
      environment: Platform.environment,
    );
    await manager.change(
      args[0] == 'autostart-enable'
          ? IntegrationChange.startGui
          : IntegrationChange.startDisable,
    );
    await manager.close();
    return;
  }
  final info = jsonDecode(runtime.readAsStringSync()) as Map<String, dynamic>;
  final client = await WebSocketIpcClient.connect(
    GuiBootstrap(
      endpoint: Uri.parse(info['endpoint'] as String),
      token: info['token'] as String,
      productVersion: info['productVersion'] as String,
      buildId: info['buildId'] as String,
      daemonPid: info['pid'] as int,
      mode: 'desktop',
    ),
  );
  try {
    final status = await client.daemonStatus();
    if (status.mode != 'desktop') throw StateError('lost desktop owner');
    // Retained read-only APIs use accepted facts, not active route/IP probes.
    await client.networkInterfaces();
    final exported = await client.diagnosticsExport();
    if (exported.schemaVersion != 1 ||
        exported.productVersion != info['productVersion']) {
      throw StateError('diagnostic export contract lost');
    }
    final removed = await WebSocket.connect(
      info['endpoint'] as String,
      headers: {
        'Authorization': 'Bearer ${info['token']}',
        'Sidravia-Build-ID': info['buildId'],
      },
    );
    try {
      removed.add(
        jsonEncode({
          'kind': 'request',
          'id': 'removed-feature',
          'method': 'network.diagnose',
          'payload': {'configurationId': 'fixture-only'},
        }),
      );
      final response = jsonDecode(
        await removed.first.timeout(const Duration(seconds: 5)) as String,
      ) as Map;
      if (response['ok'] != false ||
          (response['error'] as Map)['code'] != 'unknown_method') {
        throw StateError('removed network diagnosis is still reachable');
      }
    } finally {
      await removed.close();
    }
    if (args[0] == 'prepare') {
      final config = await client.configurationCreate(
        networkBindingPolicy: const NetworkBindingPolicy.automatic(),
        institutionProfileId: 'linux-loopback',
        username: 'fictional-portable',
        password: 'fictional-only',
        autoLogin: false,
        autoReconnect: false,
        allowInsecureStorage: true,
      );
      await client.configurationUpdate(
        configurationId: config.id,
        institutionProfileId: 'linux-loopback',
        username: 'fictional-updated',
        allowInsecureStorage: true,
      );
      final store = FileGuiSettingsStore(
        '${runtime.parent.path}/gui-settings.json',
      );
      await store.read();
      await store.save(
        const GuiSettings(appearanceMode: GuiAppearanceMode.dark),
      );
    } else {
      final configs = await client.configurationList();
      if (configs.single.username != 'fictional-updated') {
        throw StateError('configuration not restored');
      }
      final store = FileGuiSettingsStore(
        '${runtime.parent.path}/gui-settings.json',
      );
      if ((await store.read())?.appearanceMode != GuiAppearanceMode.dark) {
        throw StateError('preferences not restored');
      }
      if (args[0] == 'cleanup') {
        final session = await client.sessionStartConfiguration(
          configs.single.id,
        );
        await client.sessionStop(session.id);
        await client.sessionRemove(session.id);
        await client.configurationRemove(
          configs.single.id,
          allowInsecureStorage: true,
        );
      } else if (args[0] != 'verify') {
        throw StateError('invalid operation');
      }
    }
    stdout.writeln('portable-contract-${args[0]}-passed');
  } finally {
    await client.close();
  }
}

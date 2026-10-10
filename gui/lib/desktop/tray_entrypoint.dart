import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sidravia_gui/desktop/tray_popover.dart';
import 'package:sidravia_gui/shared/theme/app_theme.dart';

const trayViewChannelName = 'sidravia/tray-view';

@pragma('vm:entry-point')
void runTrayPopover() => runApp(const TrayPopoverApp());

class TrayPopoverApp extends StatefulWidget {
  const TrayPopoverApp({super.key, this.initialData});
  final TrayPopoverData? initialData;

  @override
  State<TrayPopoverApp> createState() => _TrayPopoverAppState();
}

class _TrayPopoverAppState extends State<TrayPopoverApp> {
  static const _channel = MethodChannel(trayViewChannelName);
  final _popoverKey = GlobalKey();
  TrayPopoverData? _data;
  String? _channelError;
  int _lastHeight = 0;
  int _presentationRevision = 0;

  @override
  void initState() {
    super.initState();
    _data = widget.initialData;
    _channel.setMethodCallHandler(_handleNativeCall);
    unawaited(_loadInitialData());
  }

  Future<void> _loadInitialData() async {
    final revision = _presentationRevision;
    try {
      final value = await _channel.invokeMethod<Object?>('getPresentation');
      final next = TrayPopoverData.fromMap(value);
      if (mounted && next != null && revision == _presentationRevision) {
        setState(() => _data = next);
      }
    } on PlatformException {
      _showChannelFailure();
    } on MissingPluginException {
      _showChannelFailure();
    }
  }

  void _showChannelFailure() {
    if (mounted) {
      setState(() {
        _channelError = '托盘操作暂不可用，请双击图标显示主界面后重试。';
      });
    }
  }

  Future<void> _invokeNative(String method, [Object? arguments]) async {
    try {
      await _channel.invokeMethod<void>(method, arguments);
      if (mounted && _channelError != null) {
        setState(() => _channelError = null);
      }
    } on PlatformException {
      _showChannelFailure();
    } on MissingPluginException {
      _showChannelFailure();
    }
  }

  Future<Object?> _handleNativeCall(MethodCall call) async {
    if (call.method == 'presentationChanged') {
      _presentationRevision++;
      final next = TrayPopoverData.fromMap(call.arguments);
      if (mounted && next != null) setState(() => _data = next);
      return null;
    }
    if (call.method == 'dismiss') {
      if (mounted) setState(() => _data = null);
      return null;
    }
    return null;
  }

  void _scheduleMeasure() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final measured = _popoverKey.currentContext?.size?.height.round();
      final height = measured?.clamp(120, 700).toInt();
      if (height == null || height <= 0 || height == _lastHeight) return;
      _lastHeight = height;
      unawaited(_invokeNative('resizeToContent', height));
    });
  }

  void _sendAction(TrayIntent intent) {
    unawaited(_invokeNative('action', intent.toMap()));
  }

  @override
  void dispose() {
    _channel.setMethodCallHandler(null);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final data = _data;
    final dark = data?.dark ?? false;
    if (data != null) _scheduleMeasure();
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      darkTheme: AppTheme.dark(),
      themeMode: dark ? ThemeMode.dark : ThemeMode.light,
      home: Scaffold(
        backgroundColor: dark ? AppColors.surfaceDark : AppColors.surfaceLight,
        body: data == null
            ? const SizedBox.shrink()
            : SingleChildScrollView(
                physics: const ClampingScrollPhysics(),
                child: Column(
                  key: _popoverKey,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    TrayPopover(
                      data: data,
                      onAction: _sendAction,
                      onDismiss: () => unawaited(_invokeNative('dismiss')),
                    ),
                    if (_channelError != null)
                      SizedBox(
                        width: 210,
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(8, 6, 8, 4),
                          child: Text(
                            _channelError!,
                            style: TextStyle(
                              color: Theme.of(context).colorScheme.error,
                              fontSize: 10,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
      ),
    );
  }
}

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
      // Native keeps the popup hidden when its first presentation is absent.
    } on MissingPluginException {
      // The standalone view is only created by the Windows owner.
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
      final height = _popoverKey.currentContext?.size?.height.round();
      if (height == null || height <= 0 || height == _lastHeight) return;
      _lastHeight = height;
      unawaited(_channel.invokeMethod<void>('resizeToContent', height));
    });
  }

  void _sendAction(TrayIntent intent) {
    unawaited(_channel.invokeMethod<void>('action', intent.toMap()));
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
            : LayoutBuilder(
                builder: (context, constraints) => SingleChildScrollView(
                  physics: const ClampingScrollPhysics(),
                  child: TrayPopover(
                    key: _popoverKey,
                    data: data,
                    onAction: _sendAction,
                    onDismiss: () =>
                        unawaited(_channel.invokeMethod<void>('dismiss')),
                  ),
                ),
              ),
      ),
    );
  }
}

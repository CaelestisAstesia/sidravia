import 'package:flutter/foundation.dart';
import 'package:sidravia_gui/application/connection_presentation.dart';

/// A small immutable projection of the existing connection read model.
/// It crosses the desktop channel only as display data and typed user intent.
@immutable
class TrayPopoverData {
  const TrayPopoverData({
    required this.statusTitle,
    required this.dark,
    required this.institution,
    required this.username,
    required this.statusDetail,
    required this.statusContext,
    required this.glyph,
    required this.tone,
    required this.primaryLabel,
    required this.primaryAction,
    required this.primaryEnabled,
    required this.disabledReason,
    this.establishedAt,
    this.secondaryLabel,
    this.secondaryAction,
    this.secondaryEnabled = false,
  });

  final DateTime? establishedAt;
  String detailAt(DateTime now) {
    final at = establishedAt;
    if (at == null) return statusDetail;
    final minutes = now.difference(at).inMinutes.clamp(0, 999999999);
    return minutes < 1 ? '刚刚连接' : '${minutes ~/ 60}时${minutes % 60}分';
  }

  factory TrayPopoverData.fromPresentation(
    ConnectionPresentation p, {
    required bool dark,
  }) {
    final canShowSecondary =
        (p.statusTitle == '等待重试' &&
            p.secondaryAction.kind == GuiConnectionActionKind.stop) ||
        (p.statusTitle == '认证失败' &&
            p.secondaryAction.kind ==
                GuiConnectionActionKind.editConfiguration);
    return TrayPopoverData(
      statusTitle: p.statusTitle,
      dark: dark,
      institution: p.institution,
      username: p.username,
      establishedAt: p.tone == HomeTone.success ? p.establishedAt : null,
      statusDetail: p.tone == HomeTone.success
          ? p.statusDetail.replaceFirst('认证成功 · ', '')
          : p.statusDetail,
      statusContext: p.statusContext,
      glyph: p.glyph,
      tone: p.tone,
      primaryLabel: p.primaryLabel,
      primaryAction: TrayIntent.fromConnectionAction(p.primaryAction),
      primaryEnabled: p.primaryEnabled,
      disabledReason: p.actionError ?? '当前操作暂不可用。',
      secondaryLabel: canShowSecondary ? p.secondaryLabel : null,
      secondaryAction: canShowSecondary
          ? TrayIntent.fromConnectionAction(p.secondaryAction)
          : null,
      secondaryEnabled: canShowSecondary && p.secondaryEnabled,
    );
  }

  final String statusTitle;
  final bool dark;
  final String institution;
  final String username;
  final String statusDetail;
  final String statusContext;
  final String glyph;
  final HomeTone tone;
  final String primaryLabel;
  final TrayIntent primaryAction;
  final bool primaryEnabled;
  final String disabledReason;
  final String? secondaryLabel;
  final TrayIntent? secondaryAction;
  final bool secondaryEnabled;

  Map<String, Object?> toMap() => {
    'statusTitle': statusTitle,
    'dark': dark,
    'institution': institution,
    'username': username,
    'statusDetail': statusDetail,
    if (establishedAt != null)
      'establishedAt': establishedAt!.toUtc().toIso8601String(),
    'statusContext': statusContext,
    'glyph': glyph,
    'tone': tone.name,
    'primaryLabel': primaryLabel,
    'primaryAction': primaryAction.toMap(),
    'primaryEnabled': primaryEnabled,
    'disabledReason': disabledReason,
    'secondaryLabel': secondaryLabel,
    'secondaryAction': secondaryAction?.toMap(),
    'secondaryEnabled': secondaryEnabled,
  };

  static TrayPopoverData? fromMap(Object? value) {
    if (value is! Map<Object?, Object?>) return null;
    final title = value['statusTitle'];
    final dark = value['dark'];
    final institution = value['institution'];
    final username = value['username'];
    final detail = value['statusDetail'];
    final context = value['statusContext'];
    final glyph = value['glyph'];
    final toneName = value['tone'];
    final primaryLabel = value['primaryLabel'];
    final primaryEnabled = value['primaryEnabled'];
    final reason = value['disabledReason'];
    final primaryAction = TrayIntent.fromMap(value['primaryAction']);
    final tone = HomeTone.values.where((v) => v.name == toneName).firstOrNull;
    if (title is! String ||
        dark is! bool ||
        institution is! String ||
        username is! String ||
        detail is! String ||
        context is! String ||
        glyph is! String ||
        primaryLabel is! String ||
        primaryEnabled is! bool ||
        reason is! String ||
        tone == null ||
        primaryAction == null) {
      return null;
    }
    final rawAt = value['establishedAt'];
    final at = rawAt is String ? DateTime.tryParse(rawAt) : null;
    if (rawAt != null && at == null) return null;
    final secondaryLabel = value['secondaryLabel'];
    final secondaryEnabled = value['secondaryEnabled'];
    final secondaryAction = value['secondaryAction'] == null
        ? null
        : TrayIntent.fromMap(value['secondaryAction']);
    if (secondaryLabel != null && secondaryLabel is! String ||
        secondaryEnabled is! bool ||
        (value['secondaryAction'] != null && secondaryAction == null)) {
      return null;
    }
    return TrayPopoverData(
      establishedAt: at,
      statusTitle: title,
      dark: dark,
      institution: institution,
      username: username,
      statusDetail: detail,
      statusContext: context,
      glyph: glyph,
      tone: tone,
      primaryLabel: primaryLabel,
      primaryAction: primaryAction,
      primaryEnabled: primaryEnabled,
      disabledReason: reason,
      secondaryLabel: secondaryLabel as String?,
      secondaryAction: secondaryAction,
      secondaryEnabled: secondaryEnabled,
    );
  }
}

enum TrayIntentKind {
  connection,
  addConfiguration,
  editConfiguration,
  showSettings,
  showDetails,
  restoreMain,
  exit,
}

@immutable
class TrayIntent {
  const TrayIntent(this.kind, {this.connectionAction, this.targetId});

  factory TrayIntent.connection(GuiConnectionAction action) => TrayIntent(
    TrayIntentKind.connection,
    connectionAction: action.kind,
    targetId: action.targetId,
  );

  factory TrayIntent.fromConnectionAction(GuiConnectionAction action) =>
      switch (action.kind) {
        GuiConnectionActionKind.start ||
        GuiConnectionActionKind.reconnect ||
        GuiConnectionActionKind.stop ||
        GuiConnectionActionKind.startManagedBackend ||
        GuiConnectionActionKind.retryIpc => TrayIntent.connection(action),
        GuiConnectionActionKind.addConfiguration => const TrayIntent(
          TrayIntentKind.addConfiguration,
        ),
        GuiConnectionActionKind.editConfiguration => TrayIntent(
          TrayIntentKind.editConfiguration,
          targetId: action.targetId,
        ),
        GuiConnectionActionKind.showSettings => const TrayIntent(
          TrayIntentKind.showSettings,
        ),
        GuiConnectionActionKind.showDetails => const TrayIntent(
          TrayIntentKind.showDetails,
        ),
      };

  final TrayIntentKind kind;
  final GuiConnectionActionKind? connectionAction;
  final String? targetId;

  Map<String, Object?> toMap() => {
    'kind': kind.name,
    if (connectionAction != null) 'connectionKind': connectionAction!.name,
    if (targetId != null) 'targetId': targetId,
  };

  static TrayIntent? fromMap(Object? value) {
    if (value is! Map<Object?, Object?> || value['kind'] is! String) {
      return null;
    }
    final kind = TrayIntentKind.values
        .where((v) => v.name == value['kind'])
        .firstOrNull;
    if (kind == null) return null;
    if (kind == TrayIntentKind.connection) {
      final action = GuiConnectionActionKind.values
          .where((v) => v.name == value['connectionKind'])
          .firstOrNull;
      final id = value['targetId'];
      const connectionActions = {
        GuiConnectionActionKind.start,
        GuiConnectionActionKind.reconnect,
        GuiConnectionActionKind.stop,
        GuiConnectionActionKind.retryIpc,
        GuiConnectionActionKind.startManagedBackend,
      };
      if (action == null ||
          !connectionActions.contains(action) ||
          (id != null && id is! String) ||
          (const {
                GuiConnectionActionKind.retryIpc,
                GuiConnectionActionKind.startManagedBackend,
              }.contains(action)
              ? id != null
              : id == null)) {
        return null;
      }
      return TrayIntent(
        kind,
        connectionAction: action,
        targetId: id as String?,
      );
    }
    final targetId = value['targetId'];
    if (kind == TrayIntentKind.editConfiguration) {
      if (targetId is! String || targetId.trim().isEmpty) return null;
      return TrayIntent(kind, targetId: targetId);
    }
    if (targetId != null || value.containsKey('connectionKind')) return null;
    return TrayIntent(kind);
  }
}

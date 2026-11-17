import 'dart:ui' show ImageFilter;

import 'package:flutter/material.dart';

import 'design_header.dart';

class DesignPage extends StatelessWidget {
  const DesignPage({
    super.key,
    required this.children,
    this.pageKey,
    this.maxWidth = 620,
    this.horizontalPadding,
  });
  final Key? pageKey;
  final double maxWidth;
  final double? horizontalPadding;

  /// Preserve reference spacing through 620px, then reach the wide endpoint
  /// continuously over the available parent width (not the capped page width).
  static EdgeInsets paddingForWidth(double width) {
    final t = ((width - 620) / (758 - 620)).clamp(0.0, 1.0);
    return EdgeInsets.fromLTRB(
      24 + 12 * t,
      12 + 4 * t,
      24 + 12 * t,
      26 + 6 * t,
    );
  }

  final List<Widget> children;
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final basePadding = paddingForWidth(constraints.maxWidth);
      final pagePadding = horizontalPadding == null
          ? basePadding
          : basePadding.copyWith(
              left: horizontalPadding,
              right: horizontalPadding,
            );
      final header = children.firstOrNull is DesignHeader
          ? children.first
          : null;
      if (header != null) {
        if (Theme.of(context).platform == TargetPlatform.windows) {
          return _WindowsGlassPage(
            pageKey: pageKey,
            maxWidth: maxWidth,
            padding: pagePadding,
            header: header,
            children: children.skip(1).toList(),
          );
        }
        final padding = pagePadding;
        return Column(
          children: [
            Align(
              alignment: Alignment.topCenter,
              child: ConstrainedBox(
                key: pageKey,
                constraints: BoxConstraints(maxWidth: maxWidth),
                child: Padding(
                  padding: EdgeInsets.fromLTRB(
                    padding.left,
                    padding.top,
                    padding.right,
                    0,
                  ),
                  child: SizedBox(width: double.infinity, child: header),
                ),
              ),
            ),
            Expanded(
              child: ScrollConfiguration(
                behavior: ScrollConfiguration.of(context)
                    .copyWith(scrollbars: false),
                child: SingleChildScrollView(
                  child: Align(
                    alignment: Alignment.topCenter,
                    child: ConstrainedBox(
                      constraints: BoxConstraints(maxWidth: maxWidth),
                      child: Padding(
                        padding: EdgeInsets.fromLTRB(
                          padding.left,
                          0,
                          padding.right,
                          padding.bottom,
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: children.skip(1).toList(),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        );
      }
      return ScrollConfiguration(
        behavior: ScrollConfiguration.of(context).copyWith(scrollbars: false),
        child: SingleChildScrollView(
          child: Align(
            alignment: Alignment.topCenter,
            child: ConstrainedBox(
              key: pageKey,
              constraints: BoxConstraints(
                maxWidth: maxWidth,
                minHeight: constraints.maxHeight,
              ),
              child: Padding(
                padding: pagePadding,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: children,
                ),
              ),
            ),
          ),
        ),
      );
    },
  );
}

/// Flutter-rendered content glass; no native window-backdrop integration.
class _WindowsGlassPage extends StatefulWidget {
  const _WindowsGlassPage({
    required this.pageKey,
    required this.maxWidth,
    required this.padding,
    required this.header,
    required this.children,
  });
  final Key? pageKey;
  final double maxWidth;
  final EdgeInsets padding;
  final Widget header;
  final List<Widget> children;

  @override
  State<_WindowsGlassPage> createState() => _WindowsGlassPageState();
}

class _WindowsGlassPageState extends State<_WindowsGlassPage> {
  final _scroll = ScrollController();

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  Widget _content(Widget child, EdgeInsets padding, {Key? key}) => Align(
    alignment: Alignment.topCenter,
    child: ConstrainedBox(
      key: key,
      constraints: BoxConstraints(maxWidth: widget.maxWidth),
      child: Padding(
        padding: padding,
        child: SizedBox(width: double.infinity, child: child),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.of(context);
    final solid = media.highContrast || media.disableAnimations;
    final background = Theme.of(context).scaffoldBackgroundColor;
    final padding = widget.padding;
    return ScrollConfiguration(
      behavior: ScrollConfiguration.of(context).copyWith(scrollbars: false),
      child: CustomScrollView(
        controller: _scroll,
        slivers: [
          PinnedHeaderSliver(
            child: AnimatedBuilder(
              animation: _scroll,
              child: _content(
                widget.header,
                EdgeInsets.fromLTRB(
                  padding.left,
                  padding.top,
                  padding.right,
                  0,
                ),
                key: widget.pageKey,
              ),
              builder: (context, child) {
                final progress = _scroll.hasClients
                    ? (_scroll.offset / 32).clamp(0.0, 1.0)
                    : 0.0;
                return ClipRect(
                  child: Stack(
                    children: [
                      Positioned.fill(
                        child: solid
                            ? ColoredBox(color: background)
                            : ShaderMask(
                                blendMode: BlendMode.dstIn,
                                shaderCallback: (rect) => const LinearGradient(
                                  begin: Alignment.topCenter,
                                  end: Alignment.bottomCenter,
                                  colors: [
                                    Colors.white,
                                    Colors.white,
                                    Colors.transparent,
                                  ],
                                  stops: [0, .72, 1],
                                ).createShader(rect),
                                child: BackdropFilter(
                                  enabled: progress > 0,
                                  filter: ImageFilter.blur(
                                    sigmaX: 12 * progress,
                                    sigmaY: 12 * progress,
                                  ),
                                  child: ColoredBox(
                                    color: background.withValues(
                                      alpha: .88 * progress,
                                    ),
                                  ),
                                ),
                              ),
                      ),
                      child!,
                    ],
                  ),
                );
              },
            ),
          ),
          SliverToBoxAdapter(
            child: _content(
              Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: widget.children,
              ),
              EdgeInsets.fromLTRB(
                padding.left,
                0,
                padding.right,
                padding.bottom,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

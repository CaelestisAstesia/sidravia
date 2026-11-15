import 'package:flutter/material.dart';

/// Shared compact search control for local, already loaded information.
class DesignSearchField extends StatelessWidget {
  const DesignSearchField({
    super.key,
    required this.controller,
    required this.hint,
    required this.onChanged,
  });
  final TextEditingController controller;
  final String hint;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final touch =
        Theme.of(context).platform == TargetPlatform.android ||
        Theme.of(context).platform == TargetPlatform.iOS;
    final minimumHeight = touch ? 48.0 : 42.0;
    return Material(
      color: Colors.transparent,
      child: ListenableBuilder(
        listenable: controller,
        builder: (context, _) => TextField(
          controller: controller,
          onChanged: onChanged,
          style: const TextStyle(fontSize: 12, height: 1.45),
          decoration: InputDecoration(
            hintText: hint,
            isDense: true,
            contentPadding: const EdgeInsets.symmetric(
              horizontal: 12,
              vertical: 11,
            ),
            prefixIcon: const Icon(Icons.search, size: 18),
            prefixIconConstraints: BoxConstraints(
              minWidth: 38,
              minHeight: minimumHeight,
            ),
            constraints: BoxConstraints(minHeight: minimumHeight),
            suffixIconConstraints: BoxConstraints(
              minWidth: minimumHeight,
              minHeight: minimumHeight,
            ),
            suffixIcon: controller.text.isEmpty
                ? null
                : IconButton(
                    tooltip: '清除查找',
                    constraints: BoxConstraints.tightFor(
                      width: minimumHeight,
                      height: minimumHeight,
                    ),
                    onPressed: () {
                      controller.clear();
                      onChanged('');
                    },
                    icon: const Icon(Icons.close, size: 17),
                  ),
          ),
        ),
      ),
    );
  }
}

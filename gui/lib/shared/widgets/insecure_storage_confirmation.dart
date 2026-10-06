import 'package:flutter/material.dart';
import 'package:sidravia_gui/application/gui_operation.dart';

Future<bool> confirmInsecureStorage(
  BuildContext context,
  GuiOperation operation,
) async {
  if (!context.mounted) return false;
  return await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text('允许未受保护的存储？'),
          content: Text(operation.securityWarning),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            TextButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('仅本次允许'),
            ),
          ],
        ),
      ) ==
      true;
}

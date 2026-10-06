enum GuiOperation {
  createConfiguration('创建连接配置'),
  updateConfiguration('保存配置更改'),
  updatePassword('保存新密码'),
  autoLogin('修改自动登录'),
  autoReconnect('修改自动重连'),
  deleteConfiguration('删除登录配置'),
  connection('连接操作');

  const GuiOperation(this.label);
  final String label;
  String get insecureStorageGuidance => switch (this) {
    GuiOperation.createConfiguration ||
    GuiOperation.updatePassword => '$label需要保存凭据，但安全保护不可用，因此操作已取消。',
    _ => '$label需要重写包含凭据的配置存储，但安全保护不可用，因此操作已取消。',
  };
  String get securityWarning =>
      '安全保护不可用。继续$label会将包含账号和凭据的配置存储以未受保护的形式写入磁盘，其他能够读取该文件的用户或程序可能获取密码。仅本次操作获得授权。';
}

typedef InsecureStorageConfirmation = Future<bool> Function(
  GuiOperation operation,
);

class GuiMutationResult {
  const GuiMutationResult(this.succeeded, [this.errorCode]);
  final bool succeeded;
  final String? errorCode;
}

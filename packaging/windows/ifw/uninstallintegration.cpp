/* Copyright (C) 2026 The Qt Company Ltd.
 *
 * SPDX-License-Identifier: LicenseRef-Qt-Commercial OR GPL-3.0-only WITH Qt-GPL-exception-1.0
*/

#include "errors.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>
#include <QProcess>

namespace QInstaller {
// Full uninstallation only. An update keeps the user's PATH and startup
// settings. The installed manager owns their validation and removal rules.
void sidraviaRemoveUserIntegration(const QString &directory)
{
    const QDir installed(QCoreApplication::applicationDirPath());
    if (QDir(directory).canonicalPath().compare(installed.canonicalPath(), Qt::CaseInsensitive) != 0)
        throw Error(QObject::tr("Sidravia could not confirm the uninstall directory."));
    const QString manager = installed.filePath(QStringLiteral("sidraviamgr.exe"));
    const QFileInfo info(manager);
    if (!info.isFile() || info.isSymLink()) {
        qWarning("Sidravia user integration manager is unavailable; its registrations may need manual cleanup.");
        return;
    }
    for (const auto &feature : {QStringLiteral("autostart"), QStringLiteral("path")}) {
        QProcess process;
        process.setWorkingDirectory(directory);
        process.start(manager, {feature, QStringLiteral("disable")});
        if (!process.waitForStarted(5000)) {
            qWarning("Sidravia user integration manager could not start; its registrations may need manual cleanup.");
            return;
        }
        if (!process.waitForFinished(15000)) {
            process.kill();
            process.waitForFinished(3000);
            throw Error(QObject::tr("Sidravia could not finish removing its user integration. Please try again."));
        }
        if (process.exitStatus() == QProcess::CrashExit) {
            qWarning("Sidravia user integration manager stopped unexpectedly; its registrations may need manual cleanup.");
            return;
        }
        if (process.exitCode() == 0)
            continue;
        const auto result = QJsonDocument::fromJson(process.readAllStandardOutput()).object();
        if (feature == QStringLiteral("autostart")
            && result.value(QStringLiteral("code")).toString() == QStringLiteral("scheduled_task_conflict")) {
            // Another installation or a manually created task owns this name.
            continue;
        }
        throw Error(QObject::tr("Sidravia could not remove its user integration. Please try again."));
    }
}
} // namespace QInstaller

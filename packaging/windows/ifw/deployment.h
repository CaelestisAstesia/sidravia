/* Copyright (C) 2026 The Qt Company Ltd.
 *
 * SPDX-License-Identifier: LicenseRef-Qt-Commercial OR GPL-3.0-only WITH Qt-GPL-exception-1.0
*/

#pragma once

namespace QInstaller {
class PackageManagerCore;
class Repository;
}

namespace Sidravia {
void prepareMaintenance(bool offlineInstaller);
bool verifyRepository(const QInstaller::Repository &repository, QInstaller::PackageManagerCore *core);
}

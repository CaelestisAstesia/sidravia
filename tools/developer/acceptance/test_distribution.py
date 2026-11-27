"""Consume release files through the project's distribution contract."""

import json

from verify_distribution import verify


def test_distribution(distribution, pytestconfig):
    option = pytestconfig.getoption
    receipt_dir = option("--receipt-dir")
    filename = "verification.json" if distribution.gui else "core-verification.json"
    receipt_path = receipt_dir / filename
    assert not receipt_path.exists(), f"use a fresh receipt directory: {receipt_path}"

    receipt = verify(
        directory=distribution.directory,
        repo=option("--source-repo"),
        target=option("--target"),
        version=option("--product-version"),
        build_id=option("--build-id"),
        gui=distribution.gui,
        go=option("--go"),
        smoke=option("--native-help"),
        qt_windows_gui=distribution.gui is not None and option("--qt-windows-gui"),
    )

    assert receipt["payloadKind"] == distribution.profile
    receipt_dir.mkdir(parents=True, exist_ok=True)
    with receipt_path.open("x", encoding="utf-8") as output:
        output.write(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n")

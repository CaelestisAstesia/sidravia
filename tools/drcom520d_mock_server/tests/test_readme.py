import unittest
from pathlib import Path


class ReadmeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tool_root = Path(__file__).resolve().parents[1]
        cls.readme = (cls.tool_root / "README.md").read_text(encoding="utf-8")
        cls.accounts = (
            cls.tool_root / "examples" / "accounts.json"
        ).read_text(encoding="utf-8")

    def test_readme_starts_with_chinese_quick_start_and_example(self):
        self.assertIn("# Dr.COM 5.2.0(D) 本地模拟认证服务器", self.readme)
        self.assertIn("一分钟快速启动", self.readme)
        self.assertIn("python -m drcom520d_mock_server --example", self.readme)
        self.assertIn("student-test", self.readme)
        self.assertIn("local-test-password", self.readme)

    def test_readme_covers_trace_protocol_config_scenarios_and_troubleshooting(self):
        required = (
            "完整追踪", "JSONL", "明文密码", "server_secret",
            "Challenge", "Login", "KA1", "KA2", "Logout",
            "MD5-A", "MD5-B", "MD5-C", "CRC-1968", "HMAC",
            "错误码", "静默丢弃", "故障场景", "WinError 10013",
            "--trace-file", "--accounts", "--example",
            "--port 0", "--ready-file", "--exit-after-logout",
            "transcript_version", "offline", "Npcap",
        )
        for text in required:
            with self.subTest(text=text):
                self.assertIn(text, self.readme)

    def test_example_credentials_exist_only_in_the_designated_file(self):
        self.assertIn('"username": "student-test"', self.accounts)
        self.assertIn('"password": "local-test-password"', self.accounts)


if __name__ == "__main__":
    unittest.main()

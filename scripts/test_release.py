import unittest

from release import normalize_version, validate_next, validate_tag


class ReleaseValidationTests(unittest.TestCase):
    def test_normalization(self):
        self.assertEqual(normalize_version("v0.1.0"), "0.1.0")
        self.assertEqual(normalize_version("10.20.30"), "10.20.30")

    def test_rejects_invalid_versions_and_shell_payloads(self):
        for value in [
            "",
            "01.2.3",
            "1.02.3",
            "1.2.03",
            "1.2",
            "v1.2.3-rc.1",
            "1.2.3+meta",
            "1.2.3\n",
            " 1.2.3",
            "$(touch /tmp/pwned)",
            "1.2.3; echo injected",
            "vv1.2.3",
        ]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                normalize_version(value)

    def test_next_version_compares_numeric_not_lexical(self):
        self.assertEqual(validate_next("v0.10.0", ["v0.9.0", "v0.9.1-rc.1"]), "0.10.0")
        self.assertEqual(validate_next("0.1.0", []), "0.1.0")
        for value in ["0.9.0", "0.10.0", "0.1.0"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                validate_next(value, ["v0.10.0"])

    def test_tag_requires_matching_changelog(self):
        log = "# Changelog\n\n## [0.1.0] - 2026-10-04\n\n- Feature\n"
        self.assertEqual(validate_tag("v0.1.0", log), "0.1.0")
        for tag in ["0.1.0", "v0.2.0", "v0.1.0-rc.1"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                validate_tag(tag, log)


if __name__ == "__main__":
    unittest.main()
